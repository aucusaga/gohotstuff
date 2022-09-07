package state

import (
	"bytes"
	"fmt"
)

type VoteSet struct {
	MaxIndex      int64                   // for timeout set
	maxRound      int64                   // for vote set
	roundVoteSets map[int64]*RoundVoteSet // map[round] for vote set, map[index] for timeout set
}

type RoundVoteSet struct {
	idx        int64
	round      int64
	id         []byte
	count      map[PeerID]struct{}
	validators map[PeerID]struct{}
}

func NewVoteSet(rootRound int64) *VoteSet {
	return &VoteSet{
		maxRound:      rootRound,
		roundVoteSets: make(map[int64]*RoundVoteSet),
	}
}

// when the rollback strategy is loaded, timeout round can be similar with the previous timeout round
// (see a timeout round a of the leader a has came out, the system will rollback and rebuild round a which leader may be leader a too),
// so we use index flag to make timeout unique among the others.
func (v *VoteSet) AddRoundVoteSet(round int64, validators []PeerID, id []byte, idx int64) error {
	if v.maxRound > round {
		return fmt.Errorf("invalid round, maxRound: %d, round: %d", v.maxRound, round)
	}

	// for vote set
	if id != nil {
		if set, ok := v.roundVoteSets[round]; ok {
			if bytes.Equal(set.id, id) {
				return nil
			}
			// need reset
			return ErrVoteSetOccupied
		}
		// init new vote set for the round
		val := make(map[PeerID]struct{})
		for _, peer := range validators {
			val[peer] = struct{}{}
		}
		v.roundVoteSets[round] = &RoundVoteSet{
			idx:        -1,
			round:      round,
			id:         id,
			count:      make(map[PeerID]struct{}),
			validators: val,
		}
		return nil
	}

	// for timeout set
	set, ok := v.roundVoteSets[idx]
	if ok {
		if len(set.validators) != len(validators) {
			return fmt.Errorf("invalid validators in timeout.%d, want: %+v, have: %+v", idx, set.validators, validators)
		}
		for _, peer := range validators {
			if _, ok := set.validators[peer]; !ok {
				return fmt.Errorf("different validators in timeout.%d, want: %+v, have: %+v", idx, set.validators, validators)
			}
		}
		return nil
	}
	// init
	val := make(map[PeerID]struct{})
	for _, peer := range validators {
		val[peer] = struct{}{}
	}
	v.roundVoteSets[idx] = &RoundVoteSet{
		idx:        idx,
		round:      round,
		id:         nil,
		count:      make(map[PeerID]struct{}),
		validators: val,
	}
	return nil
}

func (v *VoteSet) Reset(round int64, validators []PeerID, id []byte, idx int64) error {
	// for vote set
	if id != nil {
		set, ok := v.roundVoteSets[round]
		if !ok {
			return v.AddRoundVoteSet(round, validators, id, -1)
		}
		set.id = id
		for pi := range set.count {
			delete(set.count, pi)
		}
		for pi := range set.validators {
			delete(set.validators, pi)
		}
		for _, peer := range validators {
			set.validators[peer] = struct{}{}
		}
		return nil
	}

	// for timeout reset
	if idx == v.MaxIndex {
		set, ok := v.roundVoteSets[idx]
		if !ok {
			goto new
		}
		if set.round != round {
			return fmt.Errorf("invalid round, idx: %d, want_round: %d, have_round: %d,", idx, set.round, round)
		}
		return nil
	}
	if idx != v.MaxIndex+1 {
		return fmt.Errorf("invalid index, max_index: %d, index: %d", v.MaxIndex, idx)
	}
new:
	v.MaxIndex = idx
	if validators == nil {
		return nil
	}
	return v.AddRoundVoteSet(round, validators, nil, v.MaxIndex)
}

func (v *VoteSet) AddVote(round int64, validator PeerID, id []byte, idx int64) error {
	// for vote set
	if id != nil {
		set, ok := v.roundVoteSets[round]
		if !ok {
			return fmt.Errorf("invalid round")
		}
		if !bytes.Equal(set.id, id) {
			return ErrVoteSetOccupied
		}
		set.count[validator] = struct{}{}
		return nil
	}

	// for timeout set
	set, ok := v.roundVoteSets[idx]
	if !ok {
		return fmt.Errorf("invalid index")
	}
	if set.idx != idx || set.round != round {
		return fmt.Errorf("invalid index or round, want_index: %d, have_index: %d, want_round: %d, have_round: %d,", set.idx, idx, set.round, round)
	}
	set.count[validator] = struct{}{}
	return nil
}

func (v *VoteSet) LoadVote(round int64, validator PeerID, id []byte, idx int64) bool {
	if id != nil {
		if set, ok := v.roundVoteSets[round]; ok {
			if bytes.Equal(set.id, id) {
				if _, ok := set.count[validator]; ok {
					return true
				}
			}
		}
		return false
	}
	if set, ok := v.roundVoteSets[idx]; ok {
		if set.round == round {
			if _, ok := set.count[validator]; ok {
				return true
			}
		}
	}
	return false
}

func (v *VoteSet) HasTwoThirdsAny(round int64, id []byte, idx int64) bool {
	var set *RoundVoteSet
	var ok bool
	if id != nil {
		set, ok = v.roundVoteSets[round]
		if !ok {
			return false
		}
		if !bytes.Equal(set.id, id) {
			return false
		}
	} else {
		set, ok = v.roundVoteSets[idx]
		if !ok {
			return false
		}
		if set.idx != idx || set.round != round {
			return false
		}
	}
	lens := 0
	for peer, _ := range set.count {
		if _, ok := set.validators[peer]; ok {
			lens++
		}
	}
	return lens > len(set.validators)*2/3
}
