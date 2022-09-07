package state

type SafetyRules interface {
	UpdatePreferredRound(round int64) bool
	CheckProposal(new, parent QuorumCert) error
	CheckVote(qc QuorumCert) error
	CheckTimeout(qc QuorumCert) error
}

type DefaultSafetyRules struct {
}
