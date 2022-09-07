package p2p

import (
	"bufio"
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
	defaultFlushTicker             = 100 * time.Millisecond
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

	send         chan struct{}
	flushTicker  *time.Ticker
	quit         chan struct{}
	channels     []*Channel
	channelsIdx  map[int32]*Channel
	onReceiveIdx map[Module]libs.Reactor

	reader        ggio.ReadCloser
	bufConnWriter *bufio.Writer

	log libs.Logger
}

func NewDefaultConn(peer NodeInfo, netStream network.Stream,
	onReceiveIdx map[Module]libs.Reactor, logger libs.Logger) (*DefaultConn, error) {
	if logger == nil {
		logger = logs.NewLogger()
	}
	w := bufio.NewWriter(netStream)
	rc := ggio.NewDelimitedReader(netStream, defaultMaxPacketMsgSize)

	dc := &DefaultConn{
		peer:          peer,
		stream:        netStream,
		send:          make(chan struct{}, 1),
		flushTicker:   time.NewTicker(defaultFlushTicker),
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
	dc.log.Info("send @ conn.Send, channel: %d, peer_id: %s, msgBytes: %+v", chID, dc.peer.ID(), fmt.Sprintf("%X", msgBytes))

	// Send message to channel.
	channel, ok := dc.channelsIdx[chID]
	if !ok {
		dc.log.Error("cannot send bytes, unknown channel @ conn.Send, channel: %d", chID)
		return false
	}

	success := channel.sendBytes(msgBytes)
	if success {
		// Wake up sendRoutine if necessary
		select {
		case dc.send <- struct{}{}:
		default:
		}
	} else {
		dc.log.Error("send failed @ conn.Send, channel :%d, peer_id: %s", chID, dc.peer.ID())
	}
	return success
}

func (dc *DefaultConn) IsRunning() bool {
	return true
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
	eof := dc.sendMsg()
	for !eof {
		eof = dc.sendMsg()
	}
	dc.flush()

	// Now we can close the connection

	dc.stream.Close()
}

func (dc *DefaultConn) sendRoutine() {
LOOP:
	for {
		select {
		case <-dc.flushTicker.C:
			dc.flush()
		case <-dc.send:
			// send some msgs
			eof := dc.sendMsg()
			if !eof {
				// Keep sendRoutine awake.
				select {
				case dc.send <- struct{}{}:
				default:
				}
			}
		}
		if !dc.IsRunning() {
			break LOOP
		}
	}
	// Cleanup
	close(dc.quit)
}

func (dc *DefaultConn) sendMsg() bool {
	if len(dc.channels) == 0 {
		return false
	}
	for _, channel := range dc.channels {
		// If nothing to send, skip this channel
		if !channel.isSendPending() {
			continue
		}
		if err := channel.writeMsgTo(dc.bufConnWriter); err != nil {
			dc.log.Error("failed to write @ sendMsg, channel_id: %d, err: %v", channel.id, err)
			continue
		}
	}
	return true
}

func (dc *DefaultConn) recvRoutine() {
	for {
		var packet pb.Packet
		err := dc.reader.ReadMsg(&packet)
		if err != nil {
			// stopServices was invoked and we are shutting down
			// receiving is excpected to fail since we will close the connection
			select {
			case <-dc.quit:
				dc.log.Error("meet quit @ recvRoutine, peer_id: %d, err: %v", dc.peer.ID(), err)
				break
			default:
			}
			if dc.IsRunning() {
				if err == io.EOF {
					dc.log.Info("connection meets EOF @ recvRoutine (likely by the other side), peer_id: %d", dc.peer.ID())
					break
				}
				dc.log.Error("connection failed @ recvRoutine (reading byte), peer_id: %d, err: %v", dc.peer.ID(), err)
			}
			break
		}

		// Read more depending on packet type.
		switch pkt := packet.Sum.(type) {
		case *pb.Packet_PacketMsg:
			cid := pkt.PacketMsg.ChannelId
			channel, ok := dc.channelsIdx[cid]
			if !ok || channel == nil {
				dc.log.Error("cannot find valid channel @ recvRoutine, peer_id: %d, err: %v",
					dc.peer.ID(), fmt.Errorf("unknown channel %d", cid))
				break
			}

			msgBytes, err := channel.recvMsg(pkt.PacketMsg)
			if err != nil {
				if dc.IsRunning() {
					dc.log.Error("channel recvMsg failed @ recvRoutine, peer_id: %s, channel_id: %d, err: %v",
						dc.peer.ID(), cid, err)
				}
				break
			}

			module := pkt.PacketMsg.Module
			onReceive, ok := dc.onReceiveIdx[Module(module)]
			if !ok {
				dc.log.Error("cannot find valid module @ recvRoutine, peer_id: %d, err: %v",
					dc.peer.ID(), fmt.Errorf("unknown module %s", module))
				break
			}

			if msgBytes != nil {
				dc.log.Debug("received bytes, channel: %d, msgBytes: %+v", pkt.PacketMsg.ChannelId, fmt.Sprintf("%X", msgBytes))
				onReceive.HandleFunc(cid, msgBytes)
			}
		default:
			dc.log.Error("connection failed @ recvRoutine6, peer_id: %d, err: %v",
				dc.peer.ID(), fmt.Errorf("unknown message type %v", reflect.TypeOf(&packet)))
			continue
		}
	}
}

func (dc *DefaultConn) flush() {
	err := dc.bufConnWriter.Flush()
	if err != nil {
		dc.log.Error("flush failed @ Conn.flush, err: %v", err)
	}
}

type Channel struct {
	id            int32
	conn          *DefaultConn
	sendQueue     chan []byte
	sendQueueSize int32 // atomic.
	recving       []byte
	sending       []byte

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

func (ch *Channel) writeMsgTo(w io.Writer) error {
	writer := ggio.NewDelimitedWriter(w)
	packetMsg := &pb.PacketMsg{
		ChannelId: ch.id,
		Eof:       true,
		Data:      ch.sending,
	}
	packet := &pb.Packet{
		Sum: &pb.Packet_PacketMsg{
			PacketMsg: packetMsg,
		},
	}
	err := writer.WriteMsg(packet)
	return err
}

func (ch *Channel) recvMsg(msg *pb.PacketMsg) ([]byte, error) {
	ch.recving = append(ch.recving, msg.Data...)
	if msg.Eof {
		msgBytes := ch.recving
		ch.recving = ch.recving[:0] // make([]byte, 0, ch.desc.RecvBufferCapacity)
		return msgBytes, nil
	}
	return nil, nil
}

// Returns true if any PacketMsgs are pending to be sent.
// Call before calling nextPacketMsg()
// Goroutine-safe
func (ch *Channel) isSendPending() bool {
	if len(ch.sending) == 0 {
		if len(ch.sendQueue) == 0 {
			return false
		}
		ch.sending = <-ch.sendQueue
	}
	return true
}
