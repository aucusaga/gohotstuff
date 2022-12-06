package p2p

import (
	"fmt"
	"io"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/pb"
	ggio "github.com/gogo/protobuf/io"
	"github.com/libp2p/go-libp2p-core/network"
)

const (
	defaultSendTimeout             = 3 * time.Second
	defaultMaxPacketMsgSize        = 1024
	defaultMaxPacketMsgPayloadSize = 1024
	defaultSendQueueCapacity       = 1024
	defaultRecvBufferCapacity      = 1024
)

type Module string

type RawConn interface {
	Start()
	FlushStop()
	Send(int32, []byte) bool
}

type DefaultConn struct {
	peer   NodeInfo
	stream network.Stream

	quit         chan struct{}
	channels     []*Channel
	channelsIdx  map[int32]*Channel
	onReceiveIdx map[Module]libs.Reactor

	reader        ggio.ReadCloser
	bufConnWriter ggio.WriteCloser

	log libs.Logger
}

func NewDefaultConn(peer NodeInfo, netStream network.Stream,
	onReceiveIdx map[Module]libs.Reactor, logger libs.Logger) (*DefaultConn, error) {
	if logger == nil {
		logger = logs.NewLogger()
	}
	w := ggio.NewDelimitedWriter(netStream)
	rc := ggio.NewDelimitedReader(netStream, defaultMaxPacketMsgSize)

	dc := &DefaultConn{
		peer:          peer,
		stream:        netStream,
		quit:          make(chan struct{}, 1),
		channels:      make([]*Channel, 0),
		channelsIdx:   make(map[int32]*Channel),
		onReceiveIdx:  onReceiveIdx,
		reader:        rc,
		bufConnWriter: w,
		log:           logger,
	}

	// default 0 channel
	dc.AddChannel(0)

	return dc, nil
}

func (dc *DefaultConn) AddChannel(id int32) error {
	if _, ok := dc.channelsIdx[id]; ok {
		dc.log.Warn("channel has been registered before @ AddChannel, id: %d", id)
		return nil
	}
	c := NewChannel(id, dc, dc.log)
	dc.channels = append(dc.channels, c)
	dc.channelsIdx[id] = c
	return nil
}

func (dc *DefaultConn) Start() {
	go dc.sendRoutine()
	go dc.recvRoutine()
}

func (dc *DefaultConn) Send(chID int32, msgBytes []byte) bool {
	// Send message to channel.
	channel, ok := dc.channelsIdx[chID]
	if !ok {
		dc.log.Error("cannot send bytes, unknown channel @ conn.Send, channel: %d", chID)
		return false
	}

	success := channel.sendBytes(msgBytes)
	dc.log.Info("send complete @ conn.Send, out: %v, channel :%d, peer_id: %s, msg: %s", success, chID, dc.peer.ID(), libs.GetSum(msgBytes))
	return success
}

// FlushStop replicates the logic of OnStop.
// It additionally ensures that all successful
// .Send() calls will get flushed before closing
// the connection.
func (dc *DefaultConn) FlushStop() {
	// wait until the sendRoutine exits
	// so we dont race on calling sendSomePacketMsgs
	<-dc.quit

	// Send and flush all pending msgs.
	// Since sendRoutine has exited, we can call this
	// safely
	for _, ch := range dc.channels {
		if len(ch.sendQueue) == 0 {
			continue
		}
		for len(ch.sendQueue) > 0 {
			select {
			case bytes := <-ch.sendQueue:
				ch.writeMsgTo(bytes)
			default:
			}
		}
	}

	// Now we can close the connection
	dc.stream.Close()
	close(dc.quit)
}

func (dc *DefaultConn) sendRoutine() {
	for _, ch := range dc.channels {
		ch := ch
		go func() {
			for {
				select {
				case bytes := <-ch.sendQueue:
					ch.writeMsgTo(bytes)
				case <-dc.quit:
					return
				}
			}
		}()
	}
}

// TODO: stream reset
func (dc *DefaultConn) recvRoutine() {
	for {
		var packet pb.Packet

		select {
		case <-dc.quit:
			dc.log.Error("meet quit @ recvRoutine, peer_id: %d", dc.peer.ID())
			return
		default:
			err := dc.reader.ReadMsg(&packet)
			if err != nil {
				// stopServices was invoked and we are shutting down
				// receiving is excpected to fail since we will close the connection
				if err == io.EOF {
					dc.log.Info("connection meets EOF @ recvRoutine (likely by the other side), peer_id: %d", dc.peer.ID())
					continue
				}
				dc.log.Error("connection failed @ recvRoutine (reading byte), peer_id: %s, err: %v", dc.peer.ID(), err)
				return
			}
			go dc.handlePkt(packet)
		}
	}
}

func (dc *DefaultConn) handlePkt(packet pb.Packet) {
	// Read more depending on packet type.
	switch pkt := packet.Sum.(type) {
	case *pb.Packet_PacketMsg:
		cid := pkt.PacketMsg.ChannelId
		channel, ok := dc.channelsIdx[cid]
		if !ok || channel == nil {
			dc.log.Error("cannot find valid channel @ recvRoutine, peer_id: %d, err: %v",
				dc.peer.ID(), fmt.Errorf("unknown channel %d", cid))
			return
		}
		module := pkt.PacketMsg.Module
		onReceive, ok := dc.onReceiveIdx[Module(module)]
		if !ok {
			dc.log.Error("cannot find valid module @ recvRoutine, peer_id: %d, err: %v",
				dc.peer.ID(), fmt.Errorf("unknown module %s", module))
			return
		}
		if pkt.PacketMsg.Data != nil {
			dc.log.Debug("received bytes, channel: %d, packet: %+v", pkt.PacketMsg.ChannelId, pkt.PacketMsg)
			onReceive.HandleFunc(cid, pkt.PacketMsg.Data)
		}
	default:
		dc.log.Error("connection failed @ recvRoutine, peer_id: %s, err: %v",
			dc.peer.ID(), fmt.Errorf("unknown message type %v", reflect.TypeOf(&packet)))
		return
	}
}

type Channel struct {
	id            int32
	conn          *DefaultConn
	sendQueue     chan []byte
	sendQueueSize int32 // atomic.
	recving       []byte

	maxPacketMsgPayloadSize int

	log libs.Logger
}

func NewChannel(id int32, conn *DefaultConn, log libs.Logger) *Channel {
	if log == nil {
		log = logs.NewLogger()
	}
	return &Channel{
		id:                      id,
		conn:                    conn,
		sendQueue:               make(chan []byte, defaultSendQueueCapacity),
		recving:                 make([]byte, 0, defaultRecvBufferCapacity),
		maxPacketMsgPayloadSize: defaultMaxPacketMsgPayloadSize,
		log:                     log,
	}
}

func (ch *Channel) sendBytes(bytes []byte) bool {
	select {
	case ch.sendQueue <- bytes:
		atomic.AddInt32(&ch.sendQueueSize, 1)
		return true
	case <-time.After(defaultSendTimeout):
		return false
	}
}

func (ch *Channel) writeMsgTo(bytes []byte) error {
	module, ok := libs.IDToModuleMap[ch.id]
	if !ok {
		return fmt.Errorf("channel id invalid, id: %d", ch.id)
	}
	id := libs.GenRandomID()
	packetMsg := &pb.PacketMsg{
		LogId:     fmt.Sprintf("%d", id),
		ChannelId: ch.id,
		Module:    module,
		Eof:       true,
		Data:      bytes,
	}
	packet := &pb.Packet{
		Sum: &pb.Packet_PacketMsg{
			PacketMsg: packetMsg,
		},
	}

	err := ch.conn.bufConnWriter.WriteMsg(packet)
	if err != nil {
		ch.log.Error("send fail @ conn.Send, channel: %d, to: %s, msg :%s, err: %v", ch.id, ch.conn.peer.ID(), libs.GetSum(bytes), err)
		return err
	}
	ch.log.Info("send succ @ conn.Send, channel: %d, to: %s, msg :%s", ch.id, ch.conn.peer.ID(), libs.GetSum(bytes))
	return nil
}
