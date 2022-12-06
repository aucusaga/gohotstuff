package types

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
)

type TimeoutMsg struct {
	Round       int64
	Index       int64
	ParentRound int64
	ParentID    []byte
	SendID      string
	Timestamp   int64

	PublicKey []byte
	Signature []byte
}

func (t *TimeoutMsg) Validate() error {
	return nil
}

func (t *TimeoutMsg) String() string {
	return fmt.Sprintf("round: %d, index: %d, parent_round: %d, parent_id: %s, from: %s, timestamp: %d",
		t.Round, t.Index, t.ParentRound, libs.F(t.ParentID), t.SendID, t.Timestamp)
}
