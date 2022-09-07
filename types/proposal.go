package types

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
