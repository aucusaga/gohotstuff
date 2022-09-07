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
	DeltaMillSec = 200
)

var (
	msgQueueSize = 1000

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
	timeoutSet *VoteSet
	// a Write-Ahead Log ensures we can recover from any kind of crash
	// and helps us avoid signing conflicting votes
	// wal WAL

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

	s := &State{
		crypto:        cc,
		host:          name,
		cfg:           cfg,
		peerMsgQueue:  make(chan MsgInfo, msgQueueSize),
		senderQueue:   make(chan MsgInfo, msgQueueSize),
		timeoutTicker: timeout,

		tree:       tree,
		voteSet:    NewVoteSet(cfg.StartRound),
		timeoutSet: NewVoteSet(cfg.StartRound),

		quit: make(chan struct{}),
		log:  logger,
	}

	return s, nil
}

func (s *State) SetSwitch(p2p libs.Switch) {
	s.p2p = p2p
}

func (s *State) Start() {
	// start the very first round timer
	nextRound := s.election.NextRound(s.cfg.StartRound, s.timeoutSet.MaxIndex)
	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeNextRound,
		Round:    nextRound,
		Duration: 4 * DeltaMillSec * time.Millisecond,
		Index:    s.timeoutSet.MaxIndex,
	})
	go s.receiveRoutine()
}

// Handle define consensus reactor function,
// NOTE: chID is ignored if it's unknown.
func (s *State) HandleFunc(chID int32, msgbytes []byte) {
	switch chID {
	case libs.ConsensusChannel:
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
		var m MsgInfo
		select {
		case m = <-s.peerMsgQueue:
			s.handleMsg(m)
		case m = <-s.senderQueue:
			s.schedule(m)
		case ti := <-s.timeoutTicker.Chan():
			s.localTimeout(ti)
		case <-s.quit:
			return
		}
	}
}

func (s *State) handleMsg(m MsgInfo) error {
	switch t := m.(type) {
	case *types.ProposalMsg:
		proposal := m.(*types.ProposalMsg)
		if err := s.onReceiveProposal(proposal); err != nil {
			s.log.Error("receive proposal fail @ state.handleMsg, proposal: %+v, err: %v", proposal, err)
			return nil
		}
		return nil
	case *types.VoteMsg:
		vote := m.(*types.VoteMsg)
		if err := s.onReceiveVote(vote); err != nil {
			s.log.Error("receive votes fail @ state.handleMsg, vote: %+v, err: %v", vote, err)
		}
		return nil
	case *types.TimeoutMsg:
		tmo := m.(*types.TimeoutMsg)
		if err := s.onReceiveTimeout(tmo); err != nil {
			s.log.Error("receive timeout fail @ state.handleMsg, timeout: %+v, err: %v", tmo, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown msginfo type @ state.handleMsg, type: %+v", t)
	}
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

	if s.voteSet.LoadVote(proposal.Round, s.host, proposal.ID, -1) {
		return nil
	}

	// the next leader should start a collect-timer for the next round,
	// the follower should start a new next-round-timer for waiting for the next round.
	nextRound := s.election.NextRound(proposal.Round, s.timeoutSet.MaxIndex)
	if s.election.Leader(nextRound, s.timeoutSet.MaxIndex) == s.host {
		s.timeoutTicker.ScheduleTimeout(timeoutInfo{
			Type:     TypeCollectVotes,
			Duration: 4 * DeltaMillSec * time.Millisecond, // 4delta round-trip
			Round:    nextRound,
			Index:    s.timeoutSet.MaxIndex,
		})
	} else {
		s.timeoutTicker.ScheduleTimeout(timeoutInfo{
			Type:     TypeNextRound,
			Duration: 4 * DeltaMillSec * time.Millisecond, // 4delta round-trip
			Round:    nextRound,
			Index:    s.timeoutSet.MaxIndex,
		})
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

	nextLeader := s.election.Leader(nextRound, s.timeoutSet.MaxIndex)
	s.senderQueue <- VoteMsg(proposal.Round, proposal.ID, parentRound, parentID, string(nextLeader))
	// host stores self-vote-msg even it isn't the next-round-leader
	if err := s.voteSet.AddRoundVoteSet(proposal.Round, nil, proposal.ID, -1); err != nil {
		if err != ErrVoteSetOccupied {
			return fmt.Errorf("add vote set when we first get fresh voteID @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
		}
		// check timeout round

		if err := s.voteSet.Reset(proposal.Round, nil, proposal.ID, -1); err != nil {
			return fmt.Errorf("reset vote set when we first get another voteID @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
		}
	}
	if err := s.voteSet.AddVote(proposal.Round, s.host, proposal.ID, -1); err != nil {
		return fmt.Errorf("try to add vote fail @ state.onReceiveProposal, proposal: %+v, err: %v", proposal, err)
	}
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

	validators := s.election.Validators(vote.Round, s.timeoutSet.MaxIndex)
	// rollback strategy may cause stale vote-set, timeout validation should be invoked
	// when add function returns an occupied error after which a reset is needed.
	if err := s.voteSet.AddRoundVoteSet(vote.Round, validators, vote.ID, -1); err != nil {
		if err != ErrVoteSetOccupied {
			return fmt.Errorf("add vote set when we first get fresh voteID @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
		}
		// check timeout round

		if err := s.voteSet.Reset(vote.Round, validators, vote.ID, -1); err != nil {
			return fmt.Errorf("reset vote set when we first get another voteID @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
		}
	}
	// add new vote info into the set
	if err := s.voteSet.AddVote(vote.Round, PeerID(vote.SendID), vote.ID, -1); err != nil {
		return fmt.Errorf("try to add vote fail @ state.onReceiveVote, vote: %+v, err: %v", vote, err)
	}
	if !s.voteSet.HasTwoThirdsAny(vote.Round, vote.ID, -1) {
		return nil
	}

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
	// has been handled before, drop it
	if s.timeoutSet.LoadVote(timeout.Round, PeerID(timeout.SendID), nil, timeout.Index) {
		return nil
	}

	// only to update max-timeout-index first.
	// validators is rely on timeout-index.
	if err := s.timeoutSet.Reset(timeout.Round, nil, nil, timeout.Index); err != nil {
		return fmt.Errorf("reset fail @ state.onReceiveTimeout, timeout: %+v, err: %v", timeout, err)
	}
	validators := s.election.Validators(timeout.Round, s.timeoutSet.MaxIndex)

	if err := s.timeoutSet.AddRoundVoteSet(timeout.Round, validators, nil, timeout.Index); err != nil {
		return fmt.Errorf("add timeout set fail @ state.onReceiveTimeout, timeout: %+v, err: %v", timeout, err)
	}
	if err := s.timeoutSet.AddVote(timeout.Round, PeerID(timeout.SendID), nil, timeout.Index); err != nil {
		return fmt.Errorf("try to add timeout fail @ state.onReceiveTimeout, timeout: %+v, err: %v", timeout, err)
	}
	if !s.timeoutSet.HasTwoThirdsAny(timeout.Round, nil, timeout.Index) {
		return nil
	}

	// collect 2/3 timeout msg, come to the new round
	if err := s.pacemaker.ProcessTimeoutRound(tmo, validators); err != nil {
		return fmt.Errorf("handle timeout msg fail @ state.onReceiveTimeout, timeout: %+v, err: %v", tmo, err)
	}
	s.NewRoundEvent()

	return nil
}

// localTimeout handles listening-proposal | collecting-votes timeout
func (s *State) localTimeout(ti timeoutInfo) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.log.Info("tick-tock ends, info: %+v", ti)

	switch ti.Type {
	case TypeCollectVotes:
	case TypeNextRound:
	}
	if err := s.timeoutSet.Reset(ti.Round, nil, nil, s.timeoutSet.MaxIndex+1); err != nil {
		return err
	}
	justify, err := s.tree.GetJustify()
	if err != nil {
		return err
	}
	highQC, err := s.tree.deserializeF(justify)
	if err != nil {
		return err
	}
	highRound, highID, err := highQC.Proposal()
	if err != nil {
		return err
	}

	s.senderQueue <- TimeoutMsg(int64(ti.Round), highRound, highID, s.timeoutSet.MaxIndex)
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
	default:
		return fmt.Errorf("unknown msginfo type @ state.schedule, type: %+v", t)
	}
	return nil
}

// NewRoundEvent starts a new timer for the next round and broadcasts the proposal
// msg when the host is the leader.
func (s *State) NewRoundEvent() error {
	nextRound := s.pacemaker.GetCurrentRound()
	nextLeader := s.election.Leader(nextRound, s.timeoutSet.MaxIndex)
	if nextLeader != s.host {
		s.log.Info("process new round as a follower, round: %d, want: %+v, local: %+v",
			int64(nextRound)+1, nextLeader, s.host)
		s.timeoutTicker.ScheduleTimeout(timeoutInfo{
			Type:     TypeNextRound,
			Duration: 4 * DeltaMillSec * time.Millisecond, // 4delta round-trip
			Round:    nextRound,
			Index:    s.timeoutSet.MaxIndex,
		})
		return nil
	}

	// generate a new proposal in a new round as a leader,
	// it's invoked after the host has collected full votes or full timeout qcs.
	nextID := s.GetNextID()
	justify, err := s.tree.GetJustify()
	if err != nil {
		s.log.Error("cannot get justify from block tree @ state.generateProposal, round: %d, err: %v", nextRound, err)
		return err
	}
	s.senderQueue <- ProposalMsg(nextRound, nextID, justify)

	s.timeoutTicker.ScheduleTimeout(timeoutInfo{
		Type:     TypeCollectVotes,
		Duration: 4 * DeltaMillSec * time.Millisecond, // 4delta round-trip
		Round:    nextRound,
		Index:    s.timeoutSet.MaxIndex,
	})

	return nil
}

func (s *State) GetNextID() []byte {
	return nil
}

//----------------------------------------------------------------
type ConsensusConfig struct {
	StartRound int64
	StartID    string
	StartValue []byte
}
