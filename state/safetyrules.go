package state

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
)

type SafetyRules interface {
	UpdatePreferredRound(round int64) error
	CheckProposal(new, parent QuorumCert) error
	CheckVote(qc QuorumCert) error
	CheckTimeout(qc QuorumCert) error
}

func NewDefaultSafetyRules(point *State) *DefaultSafetyRules {
	return &DefaultSafetyRules{
		ptr: point,
	}
}

type DefaultSafetyRules struct {
	ptr *State
}

func (s *DefaultSafetyRules) UpdatePreferredRound(round int64) error {
	return nil
}

// CheckProposal checks whether the proposal is valid:
// a. check the new proposal has a unique id & round,
// b. check the proposal's parent is in our tree, which means it's the one of children of the root,
func (s *DefaultSafetyRules) CheckProposal(new, parent QuorumCert) error {
	// duplicate id or round
	newR, newID, err := new.Proposal()
	if err != nil {
		return err
	}
	leader := s.ptr.election.Leader(newR, nil)
	if leader != PeerID(new.Sender()) {
		return fmt.Errorf("invalid proposal sender, qc: %s, want: %+v", new.String(), leader)
	}

	treeNode, err := s.ptr.tree.Search(newR, newID)
	if err != nil && err != libs.ErrValNotFound {
		return fmt.Errorf("new proposal invalid, new: %s", new.String())
	}
	pR, pID, err := parent.Proposal()
	if err != nil {
		return err
	}
	if treeNode != nil && (treeNode.ParentKey != libs.F(pID) || treeNode.Parent.Round != pR) {
		return fmt.Errorf("new proposal invalid, new: %s, parent: %s, treeNode: %+v", new.String(), parent.String(), treeNode)
	}

	_, err = s.ptr.tree.Search(pR, pID)
	if err != nil {
		return fmt.Errorf("cannot find the parent node in the tree, drop it")
	}

	return nil
}

func (s *DefaultSafetyRules) CheckVote(qc QuorumCert) error {
	voteR, voteID, err := qc.Proposal()
	if err != nil {
		return err
	}
	validators := s.ptr.election.Validators(voteR, nil)
	invalidSender := false
	for _, v := range validators {
		if v == PeerID(qc.Sender()) {
			invalidSender = true
			break
		}
	}
	if !invalidSender {
		return fmt.Errorf("invalid vote sender, qc: %s, validators: %+v", qc.String(), validators)
	}
	_, err = s.ptr.tree.Search(voteR, voteID)
	if err != nil {
		return fmt.Errorf("cannot find vote node in the tree, vote: %s", qc.String())
	}

	return nil
}

func (s *DefaultSafetyRules) CheckTimeout(qc QuorumCert) error {
	tmoR, _, err := qc.Proposal()
	if err != nil {
		return err
	}
	cr := s.ptr.pacemaker.GetCurrentRound()
	if cr > tmoR {
		return fmt.Errorf("stale tmo, tmo: %s, current_round: %d", qc.String(), cr)
	}
	validators := s.ptr.election.Validators(tmoR, nil)
	invalidSender := false
	for _, v := range validators {
		if v == PeerID(qc.Sender()) {
			invalidSender = true
			break
		}
	}
	if !invalidSender {
		return fmt.Errorf("invalid tmo sender, qc: %s, validators: %+v", qc.String(), validators)
	}

	return nil
}
