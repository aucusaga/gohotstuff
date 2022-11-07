package state

import (
	"sync"

	"github.com/aucusaga/gohotstuff/libs"
)

type VoteSet struct {
	latestRound   int64                             // for vote set
	latestID      []byte                            // for vote set
	roundVoteSets map[int64]map[string]RoundVoteSet // map[round][proposal_id]RoundVoteSet for vote set
	mtx           sync.Mutex
}

type RoundVoteSet struct {
	round      int64
	id         []byte
	count      map[PeerID]struct{}
	validators map[PeerID]struct{}
}

// TODO: load from wal
func NewVoteSet(rootRound int64) *VoteSet {
	rootRoundMap := make(map[string]RoundVoteSet)
	set := &VoteSet{
		latestRound:   rootRound,
		roundVoteSets: make(map[int64]map[string]RoundVoteSet),
	}
	set.roundVoteSets[rootRound] = rootRoundMap
	return set
}

func (s *VoteSet) AddVote(round int64, id []byte, sender PeerID, validators []PeerID) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.roundVoteSets[round]; !ok {
		roundMap := make(map[string]RoundVoteSet)
		s.roundVoteSets[round] = roundMap
	}
	if _, ok := s.roundVoteSets[round][string(id)]; !ok {
		rs := RoundVoteSet{
			round:      round,
			id:         id,
			count:      make(map[PeerID]struct{}),
			validators: make(map[PeerID]struct{}),
		}
		s.roundVoteSets[round][string(id)] = rs
	}
	if validators != nil {
		for _, peer := range validators {
			s.roundVoteSets[round][string(id)].validators[peer] = struct{}{}
		}
	}
	// has inserted before
	if _, ok := s.roundVoteSets[round][string(id)].count[sender]; ok {
		return nil
	}
	s.roundVoteSets[round][string(id)].count[sender] = struct{}{}
	if s.latestRound <= round {
		s.latestRound = round
		s.latestID = id
	}
	return nil
}

func (s *VoteSet) HasTwoThirdsAny(round int64, id []byte) bool {
	set := s.roundVoteSets[round][string(id)]
	lens := 0
	for peer := range set.count {
		if _, ok := set.validators[peer]; ok {
			lens++
		}
	}
	threshold := lens > len(set.validators)*2/3
	if threshold {
		s.reset(round, id)
	}
	return threshold
}

// Reset clean up the set storage.
// Now round is the highQC.
func (s *VoteSet) reset(round int64, id []byte) error {
	for pround, _ := range s.roundVoteSets {
		if pround <= round-libs.HotstuffChaindStep {
			delete(s.roundVoteSets, pround)
		}
	}
	return nil
}

type TimeoutSet struct {
	latestRound        int64                               // for vote set
	latestTimeoutIndex int64                               // for timeout set
	timeoutSets        map[int64]map[int64]RoundTimeoutSet // map[round][timeoutidx]TimeoutSet for timeout set
	mtx                sync.Mutex
}

type RoundTimeoutSet struct {
	round      int64
	index      int64
	count      map[PeerID]struct{}
	validators map[PeerID]struct{}
}

// TODO: load from wal
func NewTimeoutSet(rootRound int64, latestTimeoutIndex int64) *TimeoutSet {
	set := &TimeoutSet{
		latestRound:        rootRound,
		latestTimeoutIndex: latestTimeoutIndex,
		timeoutSets:        make(map[int64]map[int64]RoundTimeoutSet),
	}
	item := make(map[int64]RoundTimeoutSet)
	item[latestTimeoutIndex] = RoundTimeoutSet{
		round:      rootRound,
		index:      latestTimeoutIndex,
		count:      make(map[PeerID]struct{}),
		validators: make(map[PeerID]struct{}),
	}
	set.timeoutSets[rootRound] = item
	return set
}

// when the rollback strategy is loaded, timeout round can be similar with the previous timeout round
// (see a timeout round a of the leader a has came out, the system will rollback and rebuild round a which leader may be leader a too),
// so we use index flag to make timeout unique among the others.
func (s *TimeoutSet) AddTimeout(round int64, idx int64, sender PeerID, validators []PeerID) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.timeoutSets[round]; !ok {
		item := make(map[int64]RoundTimeoutSet)
		s.timeoutSets[round] = item
	}
	if _, ok := s.timeoutSets[round][idx]; !ok {
		set := RoundTimeoutSet{
			round:      round,
			index:      idx,
			count:      make(map[PeerID]struct{}),
			validators: make(map[PeerID]struct{}),
		}
		s.timeoutSets[round][idx] = set
	}
	if validators != nil {
		for _, peer := range validators {
			s.timeoutSets[round][idx].validators[peer] = struct{}{}
		}
	}
	// has inserted before
	if _, ok := s.timeoutSets[round][idx].count[sender]; ok {
		return nil
	}

	s.timeoutSets[round][idx].count[sender] = struct{}{}
	if round > s.latestRound {
		s.latestRound = round
		s.latestTimeoutIndex = idx
		return nil
	}
	if round == s.latestRound && idx > s.latestTimeoutIndex {
		s.latestTimeoutIndex = idx
	}
	return nil
}

func (s *TimeoutSet) HasTwoThirdsAny(round int64, idx int64) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	set := s.timeoutSets[round][idx]
	lens := 0
	for peer := range set.count {
		if _, ok := set.validators[peer]; ok {
			lens++
		}
	}
	return lens > len(set.validators)*2/3
}

func (s *TimeoutSet) GetCurrentTimeoutIndex() int64 {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.latestTimeoutIndex
}

func (s *TimeoutSet) Reset(round int64, idx int64) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if round > s.latestRound {
		s.latestRound = round
		s.latestTimeoutIndex = idx
		return nil
	}
	if round == s.latestRound && idx > s.latestTimeoutIndex {
		s.latestTimeoutIndex = idx
	}
	return nil
}
