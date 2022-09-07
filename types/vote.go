package types

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
