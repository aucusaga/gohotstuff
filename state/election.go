package state

type ProposerElection interface {
	Validators(round int64, timeoutIdx int64) []PeerID
	Leader(round int64, timeoutIdx int64) PeerID
	NextRound(round int64, timeoutIdx int64) int64
}

type DefaultElection struct {
}
