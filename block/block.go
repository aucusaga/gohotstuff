package block

// Block defines the interface used by the hotstuff.
type Block interface {
	PruneBlocks(id []byte) (uint64, error)
}
