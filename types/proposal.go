package types

import (
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
)

type ProposalMsg struct {
	Round         int64
	ID            []byte
	JustifyParent []byte
	PeerID        string
	Timestamp     int64

	PublicKey []byte
	Signature []byte
}

func (p *ProposalMsg) Validate() error {
	return nil
}

func (p *ProposalMsg) String() string {
	return fmt.Sprintf("round: %d, id: %s, from: %s, timestamp: %d", p.Round, libs.F(p.ID), p.PeerID, p.Timestamp)
}
