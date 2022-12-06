package types

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
)

type VoteMsg struct {
	Round       int64
	ID          []byte
	ParentRound int64
	ParentID    []byte
	CommitInfo  []byte
	SendID      string
	To          string
	Timestamp   int64

	PublicKey []byte
	Signature []byte
}

func (v *VoteMsg) Validate() error {
	return nil
}

func (v *VoteMsg) String() string {
	return fmt.Sprintf("round: %d, id: %s, parent_round: %d, parent_id: %s, from: %s, to: %s, timestamp: %d",
		v.Round, libs.F(v.ID), v.ParentRound, libs.F(v.ParentID), v.SendID, v.To, v.Timestamp)
}
