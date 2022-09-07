package state

// PacemakerInterface is the interface of Pacemaker. It responsible for generating a new round.
// We assume Pacemaker in all correct replicas will have synchronized leadership after GST.
// Safty is entirelydecoupled from liveness by any potential instantiation of Packmaker.
// Different consensus have different pacemaker implement.
type Pacemaker interface {
	// GetCurrentRound return current round of this node.
	GetCurrentRound() int64
	// AdvanceRound increase the round number when the input is valid,
	// it will invoke next_round_event().
	AdvanceRound(qc QuorumCert) error
	// ProcessTimeoutRound decides the next round when the node has enough timeoutMSG,
	// developer can choose different procedures like advance_round only or rollback to the previous round,
	// it will invoke next_round_event().
	ProcessTimeoutRound(qc QuorumCert, validators []PeerID) error
}

type DefaultPacemaker struct {
}
