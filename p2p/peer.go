package p2p

import (
	"errors"
	"sync"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/libp2p/go-libp2p-core/network"
	peer "github.com/libp2p/go-libp2p-core/peer"
)

type PeerID string

// Peer is an interface representing a peer connected on a reactor.
type Peer interface {
	RawConn
	NodeInfo
}

// PeerSet is a special structure for keeping a table of peers.
// Iteration over the peers is super fast and thread-safe.
type PeerSet struct {
	lookup map[PeerID]Peer
	list   []Peer

	mtx sync.Mutex
}

func NewPeerSet() *PeerSet {
	return &PeerSet{
		lookup: make(map[PeerID]Peer),
		list:   make([]Peer, 0),
	}
}

func (s *PeerSet) Range(f func(Peer) bool) chan bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	ch := make(chan bool, len(s.list))
	go func() {
		defer close(ch)
		var wg sync.WaitGroup
		wg.Add(len(s.list))
		for _, peer := range s.list {
			peer := peer
			go func() {
				defer wg.Done()
				ch <- f(peer)
			}()
		}
		wg.Wait()
	}()

	return ch
}

func (s *PeerSet) Add(peer Peer) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	var newOne []Peer
	// delete the old one
	if _, ok := s.lookup[peer.ID()]; ok {
		for _, p := range s.list {
			if p.ID() != peer.ID() {
				newOne = append(newOne, p)
			}
		}
		s.list = newOne
	}
	s.lookup[peer.ID()] = peer
	s.list = append(s.list, peer)
	return nil
}

func (s *PeerSet) Find(id PeerID) (Peer, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	peer, ok := s.lookup[id]
	if !ok {
		return nil, errors.New("peer not found")
	}
	return peer, nil
}

type DefaultPeer struct {
	*DefaultNodeInfo
	conn *DefaultConn
}

func NewDefaultPeer(peer *peer.AddrInfo, netStream network.Stream,
	onReceiveIdx map[Module]libs.Reactor, logger libs.Logger) (Peer, error) {
	// create a new logger
	if logger == nil {
		logger = logs.NewLogger()
	}
	peerInfo := &DefaultNodeInfo{
		addr: peer,
	}
	conn, err := NewDefaultConn(peerInfo, netStream, onReceiveIdx, logger)
	if err != nil {
		return nil, err
	}
	return &DefaultPeer{
		DefaultNodeInfo: peerInfo,
		conn:            conn,
	}, nil
}

func (dc *DefaultPeer) Start() {
	dc.conn.Start()
}

func (p *DefaultPeer) FlushStop() {
	p.conn.FlushStop()
}

func (p *DefaultPeer) Send(chID int32, msgBytes []byte) bool {
	return p.conn.Send(chID, msgBytes)
}
