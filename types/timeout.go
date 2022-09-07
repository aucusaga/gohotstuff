package types

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
