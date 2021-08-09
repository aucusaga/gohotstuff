package state

// State handles execution of the hotstuff consensus algorithm.
// It processes votes and proposals, and upon reaching agreement,
// commits blocks to the storage and executes them against the application.
// The internal state machine receives input from peers, the internal validator, and from a timer.
type State struct {
}
