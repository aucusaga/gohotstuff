package state

type SafetyRules interface {
	UpdatePreferredRound(round int64) error
	CheckProposal(new, parent QuorumCert) error
	CheckVote(qc QuorumCert) error
	CheckTimeout(qc QuorumCert) error
}

func NewDefaultSafetyRules() *DefaultSafetyRules {
	return &DefaultSafetyRules{}
}

type DefaultSafetyRules struct {
}

func (s *DefaultSafetyRules) UpdatePreferredRound(round int64) error {
	return nil
}

func (s *DefaultSafetyRules) CheckProposal(new, parent QuorumCert) error {
	return nil
}

func (s *DefaultSafetyRules) CheckVote(qc QuorumCert) error {
	return nil
}

func (s *DefaultSafetyRules) CheckTimeout(qc QuorumCert) error {
	return nil
}
