package storage

// Block stores the service data, which can be the command in database or the transaction in blockchain.
type Block interface {
	PruneBlocks(id []byte) (int64, err error)
	Commit(id []byte) error
}
