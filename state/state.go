package state

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/crypto"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/types"
)

const (
	DeltaMillSec = 1000
	TimeoutT     = 4 * DeltaMillSec * time.Millisecond // 4delta round-trip

	MsgQueueSize     = 1000
	NoRollbackTmoIdx = 0
)

var (
	_new_qurumcert       = NewDefaultQuorumCert
	_unmarshal_qurumcert = DefaultDeserialize
)

var (
	ErrVoteSetOccupied = errors.New("round has been occupied")
)

// State handles execution of the hotstuff consensus algorithm.
// It processes votes and proposals, and upon reaching agreement,
// commits blocks to the storage and executes them against the application.
// The internal state machine receives input from peers, the internal validator, and from a timer.
type State struct {
	p2p    libs.Switch
	crypto crypto.CryptoClient

	host PeerID
	cfg  *ConsensusConfig
	// state changes may be triggered by: msgs from peers,
	// msgs from ourself, or by timeouts
	peerMsgQueue  chan MsgInfo
	senderQueue   chan MsgInfo
	timeoutTicker TimeoutTicker

	safetyrules SafetyRules
	pacemaker   Pacemaker
	election    ProposerElection

	tree       *BlockTree
	voteSet    *VoteSet
	timeoutSet *TimeoutSet
	// a Write-Ahead Log ensures we can recover from any kind of crash
	// and helps us avoid signing conflicting votes
	// wal WAL

	// only for dropping stale msgs.
	msgBucket sync.Map

	// procedure mutex, ensures smr only handle one type msg per step.
	mtx  sync.RWMutex
	quit chan struct{}
	log  libs.Logger
}

func NewState(name PeerID, cc crypto.CryptoClient, timeout TimeoutTicker, logger libs.Logger,
	cfg *ConsensusConfig) (*State, error) {
	if logger == nil {
		logger = logs.NewLogger()
	}

	tree, err := NewQCTree(name, cfg.StartRound, cfg.StartID,
		cfg.StartValue, _unmarshal_qurumcert, _new_qurumcert)
	if err != nil {
		logger.Error("build a new tree fail @ state.NewState, err: %v", err)
		return nil, err
	}

	safetyrules := NewDefaultSafetyRules()
	pacemaker := NewDefaultPacemaker(cfg.StartRound)
	election := NewDefaultElection(cfg.StartRound, cfg.StartValidators)
	s := &State{
		crypto:        cc,
		host:          name,
		cfg:           cfg,
		peerMsgQueue:  make(chan MsgInfo, MsgQueueSize),
		senderQueue:   make(chan MsgInfo, MsgQueueSize),
		timeoutTicker: timeout,

		safetyrules: safetyrules,
		pacemaker:   pacemaker,
		election:    election,
		tree:        tree,
		voteSet:     NewVoteSet(cfg.StartRound),
		timeoutSet:  NewTimeoutSet(cfg.StartRound, cfg.StartTimeoutIdx),

		quit: make(chan struct{}),
		log:  logger,
	}

	s.log.Info("new a state succ, s: %+v", s)
	return s, nil
}

func (s *State) SetSwitch(p2p libs.Switch) {
	s.p2p = p2p
}

func (s *State) Start() {
	go s.timeoutTicker.Start()
	go s.receiveRoutine()
	// start the very first round timer
	nextRound := s.pacemaker.GetCurrentRound()
	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeNextRound,
		Round:    nextRound,
		Duration: TimeoutT,
		Index:    s.timeoutSet.GetCurrentTimeoutIndex(),
	})
}

// Handle define consensus reactor function,
// NOTE: chID is ignored if it's unknown.
func (s *State) HandleFunc(chID int32, msgbytes []byte) {
	switch chID {
	case libs.ConsensusChannel:
		s.log.Info("receive msg: %s", GetSum(msgbytes))
		msg, err := ConsMsgFromProto(msgbytes)
		if err != nil {
			s.log.Error("transfer msg from proto fail @ state.Handle, err: %v", err)
			return
		}
		s.peerMsgQueue <- msg
	default:
	}
}

func (s *State) receiveRoutine() {
	for {
		select {
		case m := <-s.peerMsgQueue:
			s.handleMsg(m)
		case m := <-s.senderQueue:
			s.schedule(m)
		case m := <-s.timeoutTicker.Chan():
			s.localTimeout(m)
		case <-s.quit:
			return
		}
	}
}

func (s *State) handleMsg(m MsgInfo) error {
	msgbytes, err := ProtoFromConsMsg(m)
	if err != nil {
		s.log.Error("ProtoFromConsMsg fail @ handleMsg,err: %v", err)
		return err
	}
	switch t := m.(type) {
	case *types.ProposalMsg:
		s.log.Info("receive proposal @ handleMsg, msg: %s, proposal: %+v, id: %s", GetSum(msgbytes), t, string(t.ID))
		if err := s.onReceiveProposal(t); err != nil {
			s.log.Error("receive proposal fail @ state.handleMsg, proposal: %+v, err: %v", t, err)
		}
	case *types.VoteMsg:
		s.log.Info("receive vote @ handleMsg, msg: %s, vote: %+v, id: %s", GetSum(msgbytes), t, string(t.ID))
		if err := s.onReceiveVote(t); err != nil {
			s.log.Error("receive votes fail @ state.handleMsg, vote: %+v, err: %v", t, err)
		}
	case *types.TimeoutMsg:
		s.log.Info("receive timeout @ handleMsg, msg: %s, timeout: %+v", GetSum(msgbytes), t)
		if err := s.onReceiveTimeout(t); err != nil {
			s.log.Error("receive timeout fail @ state.handleMsg, timeout: %+v, err: %v", t, err)
		}
	default:
		s.log.Error("unknown msginfo type @ state.handleMsg")
		return fmt.Errorf("unknown msginfo type @ state.handleMsg, type: %+v", t)
	}
	return nil
}

// onReceiveProposal enters a proposal event, which should follow below procedures:
// a. pacemaker invokes advance_round() to check whether to step to a new round,
// b. saftyrules tries to update preferred round if need,
// c. ledger checks whether to commit proposal's great grantparent qc,
// d. block tree tries to insert proposal's new qc and update fresh new high qc,
// e. send vote_msg to the next leader.
//
func (s *State) onReceiveProposal(proposal *types.ProposalMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.log.Info("new proposal @ state.onReceiveProposal, proposal: %+v", proposal)

	parentQC, err := s.tree.deserializeF(proposal.JustifyParent)
	if err != nil {
		return fmt.Errorf("unmarshal parentQC fail @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
	}
	parentRound, parentID, err := parentQC.Proposal()
	if err != nil {
		return fmt.Errorf("invalid parentQC @ state.onReceiveProposal, parentQC: %+v, err: %v", parentQC, err)
	}
	pnode, err := s.tree.Search(parentRound, parentID)
	if err != nil {
		return fmt.Errorf("cannot find parent node in our local tree, parentQC: %+v, err: %v", parentQC, err)
	}

	newQC, err := s.tree.newQurumCertF(string(proposal.PeerID), proposal.Signature,
		proposal.Round, proposal.ID, parentRound, parentID)
	if err != nil {
		return fmt.Errorf("fail to build newQC @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
	}
	if err := s.safetyrules.CheckProposal(newQC, parentQC); err != nil {
		return fmt.Errorf("check proposal fail @ state.onReceiveProposal, newQC: %+v, parentQC: %+v", newQC, parentQC)
	}

	s.safetyrules.UpdatePreferredRound(parentRound)
	if err := s.pacemaker.AdvanceRound(parentQC); err != nil {
		return fmt.Errorf("pacemaker advanceRound fail @ state.onReceiveProposal, proposal: %+v, parentQC: %+v, err: %v",
			proposal, parentQC, err)
	}
	if pnode.Parent != nil && pnode.Parent.Parent != nil {
		s.tree.ProcessCommit(pnode.Parent.Parent.ID)
	}
	if err := s.tree.ExecuteNInsert(newQC); err != nil {
		return fmt.Errorf("insert qcTree fail @ state.onReceiveProposal, newQC: %+v, err: %v", newQC, err)
	}

	nextLeader := s.election.Leader(s.pacemaker.GetCurrentRound(), nil)
	s.senderQueue <- VoteMsg(proposal.Round, proposal.ID, parentRound, parentID, string(nextLeader))
	s.NewRoundEvent()
	return nil
}

// onReceiveVote enters a vote event as a leader, which should follow below procedures:
// 1. saftyrules checks if voteMsg is valid,
// 2. collect votes and decides to updade high qc when votes numbers come to 2f+1,
// 3. pacemaker invokes advance_round().
func (s *State) onReceiveVote(vote *types.VoteMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	voteQC, err := s.tree.newQurumCertF(string(vote.SendID), vote.Signature, vote.Round,
		vote.ID, vote.ParentRound, vote.ParentID)
	if err != nil {
		return fmt.Errorf("new a vote qc fail @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
	}
	if err := s.safetyrules.CheckVote(voteQC); err != nil {
		return fmt.Errorf("check vote fail @ state.onReceiveVote, vote: %+v, err: %v", voteQC, err)
	}

	validators := s.election.Validators(vote.Round, nil)
	// add new vote info into the set
	if err := s.voteSet.AddVote(vote.Round, vote.ID, PeerID(vote.SendID), validators); err != nil {
		return fmt.Errorf("try to add vote fail @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
	}
	if !s.voteSet.HasTwoThirdsAny(vote.Round, vote.ID) {
		return nil
	}
	s.log.Info("collect 2f + 1 votes, round: %d, id: %s", vote.Round, string(vote.ID))

	// the leader has received 2/3 votes, try to advance to the next round
	if err := s.tree.ProcessVote(voteQC, validators); err != nil {
		return fmt.Errorf("still collecting @ state.onReceiveVote , vote: %+v, err: %v", vote, err)
	}
	// pacemaker advance to the next round and broadcast new proposal
	s.pacemaker.AdvanceRound(voteQC)
	s.NewRoundEvent()

	return nil
}

// onReceiveTimeout enters a timeout event for every validators, which should follow below procedures:
// 1. saftyrules checks if timeoutMsg is valid,
// 2. collect timeout and decides to refresh the next round number when timeout numbers come to 2f+1
func (s *State) onReceiveTimeout(timeout *types.TimeoutMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// timeout msg cannot predict the next id of the quromcert, so we always put a nil value.
	tmo, err := s.tree.newQurumCertF(string(timeout.SendID), timeout.Signature,
		timeout.Round, nil, timeout.ParentRound, timeout.ParentID)
	if err != nil {
		return fmt.Errorf("new a timeout qc fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo, err)
	}
	if err := s.safetyrules.CheckTimeout(tmo); err != nil {
		return fmt.Errorf("check vote fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo, err)
	}

	validators := s.election.Validators(timeout.Round, nil)
	if err := s.timeoutSet.AddTimeout(timeout.Round, timeout.Index, PeerID(timeout.SendID), validators); err != nil {
		return fmt.Errorf("try to add timeout fail @ state.onReceiveTimeout, timeout: %+v, err: %v", timeout, err)
	}
	if !s.timeoutSet.HasTwoThirdsAny(timeout.Round, timeout.Index) {
		return nil
	}

	s.log.Info("collect 2f + 1 timeout infos, round: %d, idx: %d", timeout.Round, timeout.Index)
	// collect 2/3 timeout msg, come to the new round
	if err := s.pacemaker.ProcessTimeoutRound(tmo); err != nil {
		return fmt.Errorf("still collecting timeout msg @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo, err)
	}
	s.NewRoundEvent()

	return nil
}

// localTimeout handles listening-proposal | collecting-votes timeout
func (s *State) localTimeout(ti timeoutInfo) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.log.Info("tick-tock ends, timeout info: %+v", ti)

	switch ti.Type {
	case TypeCollectVotes:
	case TypeNextRound:
	}
	if err := s.timeoutSet.Reset(ti.Round, NoRollbackTmoIdx); err != nil {
		s.log.Error("reset fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}
	justify, err := s.tree.GetJustify()
	if err != nil {
		s.log.Error("justify fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}
	highQC, err := s.tree.deserializeF(justify)
	if err != nil {
		s.log.Error("deserialize fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}
	highRound, highID, err := highQC.Proposal()
	if err != nil {
		s.log.Error("proposal fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}

	s.senderQueue <- TimeoutMsg(int64(ti.Round), highRound, highID, s.timeoutSet.GetCurrentTimeoutIndex())
	return nil
}

func (s *State) schedule(m MsgInfo) error {
	switch t := m.(type) {
	case *types.ProposalMsg:
		if PeerID(t.PeerID) == s.host {
			s.peerMsgQueue <- m
		}
		t.Timestamp = time.Now().Unix()
		t.PeerID = string(s.host)
		// sign and put pk in the msg
		msgbytes, err := ProtoFromConsMsg(t)
		if err != nil {
			return err
		}
		newmsg, err := s.crypto.Sign(msgbytes)
		if err != nil {
			return err
		}
		s.p2p.Broadcast(libs.ConsensusChannel, newmsg)
		s.log.Info("broadcast proposal msg: %s", GetSum(newmsg))
	case *types.VoteMsg:
		if PeerID(t.To) == s.host {
			s.peerMsgQueue <- m
			return nil
		}
		t.Timestamp = time.Now().Unix()
		t.SendID = string(s.host)
		// sign and put pk in the msg
		msgbytes, err := ProtoFromConsMsg(t)
		if err != nil {
			return err
		}
		newmsg, err := s.crypto.Sign(msgbytes)
		if err != nil {
			return err
		}
		p2pID, err := s.p2p.GetP2PID(t.To)
		if err != nil {
			return err
		}
		s.p2p.Send(p2pID, libs.ConsensusChannel, newmsg)
		s.log.Info("send vote msg: %s", GetSum(newmsg))
	case *types.TimeoutMsg:
		s.peerMsgQueue <- m
		t.Timestamp = time.Now().Unix()
		t.SendID = string(s.host)

		// sign and put pk in the msg
		msgbytes, err := ProtoFromConsMsg(t)
		if err != nil {
			return err
		}
		newmsg, err := s.crypto.Sign(msgbytes)
		if err != nil {
			return err
		}
		s.p2p.Broadcast(libs.ConsensusChannel, newmsg)
		s.log.Info("broadcast timeout msg: %s", GetSum(newmsg))
	default:
		return fmt.Errorf("unknown msginfo type @ state.schedule, type: %+v", t)
	}
	return nil
}

// NewRoundEvent starts a new timer for the next round and broadcasts the proposal
// msg when the host is the leader.
func (s *State) NewRoundEvent() error {
	nextRound := s.pacemaker.GetCurrentRound()
	nextLeader := s.election.Leader(nextRound, nil)
	if nextLeader != s.host {
		s.log.Info("process new round as a follower, round: %d, want: %+v, local: %+v",
			int64(nextRound), nextLeader, s.host)
		s.timeoutTicker.ScheduleTimeout(timeoutInfo{
			Type:     TypeNextRound,
			Duration: TimeoutT,
			Round:    nextRound,
			Index:    s.timeoutSet.GetCurrentTimeoutIndex(),
		})
		return nil
	}

	// generate a new proposal in a new round as a leader,
	// it's invoked after the host has collected full votes or full timeout qcs.
	nextID, err := s.GetNextID()
	if err != nil {
		s.log.Error("generate next id @ state.generateProposal, round: %d, err: %v", nextRound, err)
		return err
	}
	justify, err := s.tree.GetJustify()
	if err != nil {
		s.log.Error("cannot get justify from block tree @ state.generateProposal, round: %d, id: %s, err: %v", nextRound, string(nextID), err)
		return err
	}

	proposal := ProposalMsg(nextRound, nextID, justify)
	s.log.Info("process new round as a leader, round: %d, id: %s, proposal: %+v", int64(nextRound), string(nextID), proposal)
	s.senderQueue <- proposal
	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeCollectVotes,
		Duration: TimeoutT,
		Round:    nextRound,
		Index:    s.timeoutSet.GetCurrentTimeoutIndex(),
	})

	return nil
}

func (s *State) GetNextID() ([]byte, error) {
	id, err := libs.GenRandomID()
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("%X", id)), nil
}

//----------------------------------------------------------------
type ConsensusConfig struct {
	StartRound      int64
	StartTimeoutIdx int64
	StartID         string
	StartValue      []byte
	StartValidators []PeerID
}

func GetSum(b []byte) string {
	h := sha256.New()
	h.Write(b)
	bs := h.Sum(nil)
	return fmt.Sprintf("%x", bs)
}
