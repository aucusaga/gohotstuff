package libs

const (
	ConsensusModule  = "consensus"
	ConsensusChannel = int32(0)

	HotstuffChaindStep = 3
)

var (
	IDToModuleMap = map[int32]string{
		ConsensusChannel: ConsensusModule,
	}
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
