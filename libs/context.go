package libs

const (
	ConsensusModule  = "consensus"
	ConsensusChannel = 0
)

type Reactor interface {
	HandleFunc(chID int32, msgBytes []byte)
	SetSwitch(sw Switch)
}

type Switch interface {
	Broadcast(chID int32, msgBytes []byte)
	Send(peerID string, chID int32, msgBytes []byte) error
	GetP2PID(peerID string) (string, error)
}
