package state

import (
	"io"
)

type PeerID string

// Message is a message that can be sent and received within peers
type MsgInfo interface {
	Validate() error
}

type WALMessage interface{}

// WAL is an interface for any write-ahead logger.
type WAL interface {
	Write(WALMessage) error
	WriteSync(WALMessage) error
	FlushAndSync() error

	SearchForEndHeight(height int64) (rd io.ReadCloser, found bool, err error)

	// service methods
	Start() error
	Stop() error
	Wait()
}
