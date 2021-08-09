package goHotStuff

type Peer interface {
	SendMsg(msgType int64, msgBytes []byte)
}
