package state

import (
	"errors"
	"sync"
)

var (
	ErrNilQC = errors.New("pacemaker meets a nil qc")
)

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
	// When the builder can accept a recyclic round, which means the next round of round A can also be round A under timeout situation,
	// the next round is calculated by the specific election. Conversely it should return round + 1 when the system cannot rollback.
	ProcessTimeoutRound(qc QuorumCert) error
}

func NewDefaultPacemaker(latest int64) *DefaultPacemaker {
	return &DefaultPacemaker{
		current: latest + 1,
	}
}

type DefaultPacemaker struct {
	current int64
	mtx     sync.Mutex
}

func (p *DefaultPacemaker) GetCurrentRound() int64 {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	return p.current
}

func (p *DefaultPacemaker) AdvanceRound(qc QuorumCert) error {
	if qc == nil {
		return ErrNilQC
	}
	p.mtx.Lock()
	defer p.mtx.Unlock()

	r, _, err := qc.Proposal()
	if err != nil {
		return err
	}
	if r+1 > p.current {
		p.current = r + 1
	}
	return nil
}

// ProcessTimeoutRound DefaultPacemaker cannot rollback.
func (p *DefaultPacemaker) ProcessTimeoutRound(qc QuorumCert) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	r, _, err := qc.Proposal()
	if err != nil {
		return err
	}
	if r+1 > p.current {
		p.current = r + 1
	}
	return nil
}
