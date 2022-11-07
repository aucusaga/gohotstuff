package state

import (
	"fmt"
	"sync"
)

type ProposerElection interface {
	// roundTimeoutIdxMap map[int64]int64 is an index-look-up map
	// stores the timeout times in the same round of a rollback system.
	// round-timeoutidx can specialize the unique round on the system.
	Validators(round int64, roundTimeoutIdxMap map[int64]int64) []PeerID
	Leader(round int64, roundTimeoutIdxMap map[int64]int64) PeerID
	// User can dynamically change the validators at runtime by the commands if needed.
	Update(round int64, next []PeerID) error
}

func NewDefaultElection(start int64, init []PeerID) *DefaultElection {
	var validatorsStep []StepValidators
	validatorsStep = append(validatorsStep, StepValidators{
		Start:      start,
		Validators: init,
	})
	return &DefaultElection{
		start:          start,
		validators:     init,
		validatorsStep: validatorsStep,
	}
}

type StepValidators struct {
	Start      int64
	Validators []PeerID
}

type DefaultElection struct {
	// latest
	start      int64
	validators []PeerID

	validatorsStep []StepValidators

	mtx sync.Mutex
}

func (e *DefaultElection) Validators(round int64, roundTimeoutIdxMap map[int64]int64) []PeerID {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if round >= e.start {
		return e.validators
	}
	for i := len(e.validatorsStep) - 1; i >= 0; i-- {
		if round >= e.validatorsStep[i].Start {
			return e.validatorsStep[i].Validators
		}
	}
	return nil
}

// Leader indicates that the system cannot rollback.
func (e *DefaultElection) Leader(round int64, roundTimeoutIdxMap map[int64]int64) PeerID {
	idx := round % int64(len(e.validators))
	return e.validators[int(idx)]
}

func (e *DefaultElection) Update(round int64, next []PeerID) error {
	e.mtx.Lock()
	defer e.mtx.Unlock()

	if round <= e.start {
		return fmt.Errorf("round invalid, has been occupied")
	}
	e.validatorsStep = append(e.validatorsStep, StepValidators{round, next})
	e.start = round
	e.validators = next
	return nil
}
