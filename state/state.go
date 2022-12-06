package state

import (
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

	TimeoutProcess  = "TIMEOUT"
	ProposalProcess = "PROPOSAL"
	VoteProcess     = "VOTE"
)

var (
	_new_qurumcert       = NewDefaultQuorumCert
	_unmarshal_qurumcert = DefaultDeserialize
)

var (
	ErrVoteSetOccupied    = errors.New("round occupied")
	ErrComponentsOccupied = errors.New("components occupied")
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
	// msgBucket sync.Map

	// procedure mutex, ensures smr only handle one type msg per step.
	mtx  sync.RWMutex
	quit chan struct{}
	log  libs.Logger
}

func NewState(name PeerID, cc crypto.CryptoClient, timeout TimeoutTicker,
	logger libs.Logger, cfg *ConsensusConfig) (*State, error) {
	if logger == nil {
		logger = logs.NewLogger()
	}

	tree, err := NewQCTree(name, cfg.StartRound, cfg.StartID,
		cfg.StartValue, _unmarshal_qurumcert, _new_qurumcert, logger)
	if err != nil {
		logger.Error("build a new tree fail @ state.NewState, err: %v", err)
		return nil, err
	}

	s := &State{
		crypto:        cc,
		host:          name,
		cfg:           cfg,
		peerMsgQueue:  make(chan MsgInfo, MsgQueueSize),
		senderQueue:   make(chan MsgInfo, MsgQueueSize),
		timeoutTicker: timeout,
		tree:          tree,
		voteSet:       NewVoteSet(cfg.StartRound),
		timeoutSet:    NewTimeoutSet(cfg.StartRound, cfg.StartTimeoutIdx),
		quit:          make(chan struct{}),
		log:           logger,
	}

	s.log.Info("init a state succ, no components loaded, s: %+v", s)
	return s, nil
}

func (s *State) RegisterPaceMaker(pacemaker Pacemaker) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.pacemaker != nil {
		return ErrComponentsOccupied
	}
	s.pacemaker = pacemaker
	return nil
}

func (s *State) RegisterElection(election ProposerElection) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.election != nil {
		return ErrComponentsOccupied
	}
	s.election = election
	return nil
}

func (s *State) RegisterSaftyrules(safetyrules SafetyRules) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.safetyrules != nil {
		return ErrComponentsOccupied
	}
	s.safetyrules = safetyrules
	return nil
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
		s.log.Info("receive msg: %s", libs.GetSum(msgbytes))
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
		s.log.Info("receive proposal @ handleMsg, msg: %s, proposal: %s", libs.GetSum(msgbytes), t.String())
		if err := s.onReceiveProposal(t); err != nil {
			s.log.Error("receive proposal fail @ state.handleMsg, proposal: %+v, err: %v", t, err)
		}
	case *types.VoteMsg:
		s.log.Info("receive vote @ handleMsg, msg: %s, vote: %s", libs.GetSum(msgbytes), t.String())
		if err := s.onReceiveVote(t); err != nil {
			s.log.Error("receive votes fail @ state.handleMsg, vote: %+v, err: %v", t, err)
		}
	case *types.TimeoutMsg:
		s.log.Info("receive timeout @ handleMsg, msg: %s, timeout: %s", libs.GetSum(msgbytes), t.String())
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
func (s *State) onReceiveProposal(proposal *types.ProposalMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	parentQC, err := s.tree.DeserializeF(proposal.JustifyParent)
	if err != nil {
		return fmt.Errorf("unmarshal parentQC fail @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
	}
	parentRound, parentID, err := parentQC.Proposal()
	if err != nil {
		return fmt.Errorf("invalid parentQC @ state.onReceiveProposal, parentQC: %s, err: %v", parentQC.String(), err)
	}
	pnode, err := s.tree.Search(parentRound, parentID)
	if err != nil {
		return fmt.Errorf("cannot find parent node in our local tree, parentQC: %+v, err: %v", parentQC, err)
	}

	newQC, err := s.tree.NewQurumCertF(string(proposal.PeerID), proposal.Signature,
		proposal.Round, proposal.ID, parentRound, parentID)
	if err != nil {
		return fmt.Errorf("fail to build newQC @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
	}
	if err := s.safetyrules.CheckProposal(newQC, parentQC); err != nil {
		return fmt.Errorf("check proposal fail @ state.onReceiveProposal, newQC: %+v, parentQC: %+v", newQC, parentQC)
	}

	// atomic operations
	s.safetyrules.UpdatePreferredRound(parentRound)
	if err := s.pacemaker.AdvanceRound(parentQC); err != nil {
		return fmt.Errorf("pacemaker advanceRound fail @ state.onReceiveProposal, proposal: %+v, parentQC: %+v, err: %v",
			proposal, parentQC, err)
	}
	err = s.tree.ExecuteNInsert(newQC)
	if err != nil && err != libs.ErrRepeatInsert {
		return fmt.Errorf("insert qcTree fail @ state.onReceiveProposal, newQC: %+v, err: %v", newQC, err)
	}
	if pnode.Parent != nil && pnode.Parent.Parent != nil && pnode.Parent.Parent.Parent != nil {
		s.tree.ProcessCommit(pnode.Parent.Parent.Parent.ID)
	}
	s.log.Info("receive a proposal ticket, proposal: %s, new_round: %d, high_qc: [%s], root_qc: [%s]",
		newQC.String(), s.pacemaker.GetCurrentRound(), s.tree.GetCurrentHighQC().String(), s.tree.GetCurrentRoot().String())

	nextRound := s.pacemaker.GetCurrentRound() + 1
	nextLeader := s.election.Leader(nextRound, s.timeoutSet.GetTimeoutIdxMap())
	s.senderQueue <- VoteMsg(proposal.Round, proposal.ID, parentRound, parentID, string(nextLeader))
	return nil
}

// onReceiveVote enters a vote event as a leader, which should follow below procedures:
// 1. saftyrules checks if voteMsg is valid,
// 2. collect votes and decides to updade high qc when votes numbers come to 2f+1,
// 3. pacemaker invokes advance_round().
func (s *State) onReceiveVote(vote *types.VoteMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	voteQC, err := s.tree.NewQurumCertF(string(vote.SendID), vote.Signature, vote.Round,
		vote.ID, vote.ParentRound, vote.ParentID)
	if err != nil {
		return fmt.Errorf("new a vote qc fail @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
	}
	if err := s.safetyrules.CheckVote(voteQC); err != nil {
		return fmt.Errorf("check vote fail @ state.onReceiveVote, vote: %+v, err: %v", voteQC.String(), err)
	}
	validators := s.election.Validators(vote.Round, s.timeoutSet.GetTimeoutIdxMap())
	// add new vote info into the set
	if err := s.voteSet.AddVote(vote.Round, vote.ID, PeerID(vote.SendID), validators); err != nil {
		return fmt.Errorf("try to add vote fail @ state.onReceiveVote, vote: %+v, err: %v", voteQC.String(), err)
	}
	s.log.Info("receive a vote ticket, vote: %s, validators: %+v", voteQC.String(), validators)
	if !s.voteSet.HasTwoThirdsAny(vote.Round, vote.ID) {
		return nil
	}

	// atomic operations
	// the leader has received 2/3 votes, try to advance to the next round
	if err := s.tree.ProcessVote(voteQC, validators); err != nil {
		return fmt.Errorf("still collecting @ state.onReceiveVote , vote: %+v, err: %v", vote, err)
	}
	// pacemaker advance to the next round and broadcast new proposal
	s.pacemaker.AdvanceRound(voteQC)
	s.log.Info("collect 2f+1 votes, vote: %s, new_round: %d, high_qc: [%s]",
		voteQC.String(), s.pacemaker.GetCurrentRound(), s.tree.GetCurrentHighQC().String())
	s.NewRoundEvent(VoteProcess)
	return nil
}

// onReceiveTimeout enters a timeout event for every validators, which should follow below procedures:
// 1. saftyrules checks if timeoutMsg is valid,
// 2. collect timeout and decides to refresh the next round number when timeout numbers come to 2f+1
func (s *State) onReceiveTimeout(timeout *types.TimeoutMsg) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// timeout msg cannot predict the next id of the quromcert, so we generate an unique one
	tmo, err := s.tree.NewQurumCertF(string(timeout.SendID), timeout.Signature,
		timeout.Round, s.getTimeoutID(timeout.Round, timeout.Index), timeout.ParentRound, timeout.ParentID)
	if err != nil {
		return fmt.Errorf("new a timeout qc fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}
	if err := s.safetyrules.CheckTimeout(tmo); err != nil {
		return fmt.Errorf("check vote fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}

	validators := s.election.Validators(timeout.Round, s.timeoutSet.GetTimeoutIdxMap())
	if err := s.timeoutSet.AddTimeout(timeout.Round, timeout.Index, PeerID(timeout.SendID), validators); err != nil {
		return fmt.Errorf("try to add timeout fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}
	s.log.Info("receive a timeout ticket: %s, validators: %+v", tmo.String(), validators)
	if !s.timeoutSet.HasTwoThirdsAny(timeout.Round, timeout.Index) {
		return nil
	}

	// atomic operations
	// collect 2/3 timeout msg, come to the new round
	if err := s.pacemaker.ProcessTimeoutRound(tmo); err != nil {
		return fmt.Errorf("still collecting timeout msg @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}
	err = s.tree.ExecuteNInsert(tmo)
	if err != nil {
		if err != libs.ErrRepeatInsert {
			return fmt.Errorf("insert qcTree fail @ state.onReceiveTimeout, newQC: %+v, err: %v", tmo.String(), err)
		}
		return nil
	}
	if err := s.tree.ProcessVote(tmo, validators); err != nil {
		return fmt.Errorf("fail to update highQC @ state.onReceiveTimeout , newQC: %+v, err: %v", tmo.String(), err)
	}
	s.log.Info("collect 2f+1 tmos, tmo: %s, new_round: %d, high_qc: [%s]",
		tmo.String(), s.pacemaker.GetCurrentRound(), s.tree.GetCurrentHighQC().String())
	s.NewRoundEvent(TimeoutProcess)
	return nil
}

// localTimeout handles listening-proposal | collecting-votes timeout
func (s *State) localTimeout(ti timeoutInfo) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	justify, err := s.tree.GetJustify()
	if err != nil {
		s.log.Error("justify fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}
	highQC, err := s.tree.DeserializeF(justify)
	if err != nil {
		s.log.Error("deserialize fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}
	highRound, highID, err := highQC.Proposal()
	if err != nil {
		s.log.Error("proposal fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}

	tmo, err := s.tree.NewQurumCertF(string(s.host), nil,
		ti.Round, s.getTimeoutID(ti.Round, ti.Index), highRound, highID)
	if err != nil {
		return fmt.Errorf("new a timeout qc fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}
	// tmo info may be stale after the machine reveive vote/proposal/tmo msg
	if err := s.safetyrules.CheckTimeout(tmo); err != nil {
		return fmt.Errorf("check vote fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo.String(), err)
	}
	s.log.Info("tick-tock ends, get timeout info: %+v", ti)
	if err := s.timeoutSet.Reset(ti.Round, NoRollbackTmoIdx); err != nil {
		s.log.Error("reset fail @ local timeout, timeout info: %+v, err: %+v", ti, err)
		return err
	}

	s.senderQueue <- TimeoutMsg(int64(ti.Round), highRound, highID, s.timeoutSet.GetCurrentTimeoutIndex())
	// tmo collecting should also follow timeout rules.
	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeNextRound,
		Duration: TimeoutT,
		Round:    ti.Round,
		Index:    s.timeoutSet.GetCurrentTimeoutIndex(),
	})
	return nil
}

func (s *State) schedule(m MsgInfo) error {
	switch t := m.(type) {
	case *types.ProposalMsg:
		t.Timestamp = time.Now().Unix()
		t.PeerID = string(s.host)
		s.peerMsgQueue <- m
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
		s.log.Info("broadcast proposal msg: %s", libs.GetSum(newmsg))
	case *types.VoteMsg:
		t.Timestamp = time.Now().Unix()
		t.SendID = string(s.host)
		s.peerMsgQueue <- m
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
		s.log.Info("send vote msg: %s", libs.GetSum(newmsg))
	case *types.TimeoutMsg:
		t.Timestamp = time.Now().Unix()
		t.SendID = string(s.host)
		s.peerMsgQueue <- m
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
		s.log.Info("broadcast timeout msg: %s", libs.GetSum(newmsg))
	default:
		return fmt.Errorf("unknown msginfo type @ state.schedule, type: %+v", t)
	}
	return nil
}

// NewRoundEvent starts a new timer for the next round and broadcasts the proposal
// msg when the host is the leader.
func (s *State) NewRoundEvent(action string) error {
	nextRound := s.pacemaker.GetCurrentRound()
	nextLeader := s.election.Leader(nextRound, s.timeoutSet.GetTimeoutIdxMap())
	if nextLeader != s.host {
		s.log.Info("process new round as a follower, process: %s, round: %d, want: %+v, local: %+v",
			action, int64(nextRound), nextLeader, s.host)
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
	if action == VoteProcess || action == TimeoutProcess {
		nextID, err := s.GetNextID()
		if err != nil {
			s.log.Error("generate next id @ state.generateProposal, round: %d, err: %v", nextRound, err)
			return err
		}
		justify, err := s.tree.GetJustify()
		if err != nil {
			s.log.Error("cannot get justify from block tree @ state.generateProposal, round: %d, id: %s, err: %v", nextRound, libs.F(nextID), err)
			return err
		}

		proposal := ProposalMsg(nextRound, nextID, justify)
		s.log.Info("process new round as a leader, process: %s, round: %d, id: %s, proposal: %+v", action, int64(nextRound), libs.F(nextID), proposal.String())
		s.senderQueue <- proposal
	}

	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeCollectVotes,
		Duration: TimeoutT,
		Round:    nextRound,
		Index:    s.timeoutSet.GetCurrentTimeoutIndex(),
	})

	return nil
}

// GetNextID tries to simulate the data encapsulation.
func (s *State) GetNextID() ([]byte, error) {
	// trick, proposal rate limit
	time.Sleep(2 * time.Second)
	id := libs.GenRandomID()
	return []byte(fmt.Sprintf("%d", id)), nil
}

func (s *State) getTimeoutID(round int64, index int64) []byte {
	return []byte(fmt.Sprintf("tmo_%d_%d", round, index))
}

//----------------------------------------------------------------
type ConsensusConfig struct {
	StartRound      int64
	StartTimeoutIdx int64
	StartID         string
	StartValue      []byte
	StartValidators []PeerID
}
