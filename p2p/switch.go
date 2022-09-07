package p2p

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/libs"
	ipfsaddr "github.com/ipfs/go-ipfs-addr"
	"github.com/libp2p/go-libp2p"
	circuit "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	secio "github.com/libp2p/go-libp2p-secio"
	"github.com/multiformats/go-multiaddr"
)

const (
	protocolPrefix = "/gohotstuff/p2p"
)

var (
	defaultTickerTimeSec = 5
)

type Switch struct {
	cfg *Config

	id    *multiaddr.Multiaddr
	host  host.Host
	kdht  *dht.IpfsDHT
	peers *PeerSet
	timer *time.Ticker
	quit  chan struct{}

	reactor map[Module]libs.Reactor
	mtx     sync.Mutex

	log libs.Logger
}

func NewSwitch(cfg *Config, logger libs.Logger) (*Switch, error) {
	if cfg.TickerTimeSec == 0 {
		cfg.TickerTimeSec = int64(defaultTickerTimeSec)
	}
	sw := &Switch{
		quit:  make(chan struct{}),
		cfg:   cfg,
		peers: NewPeerSet(),
		timer: time.NewTicker(time.Duration(cfg.TickerTimeSec) * time.Second),
	}
	sw.log = logger
	if logger == nil {
		sw.log = logs.NewLogger()
	}
	return sw, nil
}

// AddReactor should be invoked before switch.Start(),
// consensus module must be registered.
func (sw *Switch) AddReactor(mo Module, f libs.Reactor) error {
	sw.mtx.Lock()
	defer sw.mtx.Unlock()

	if _, ok := sw.reactor[mo]; !ok {
		return fmt.Errorf("module has been registered before, %v", mo)
	}

	sw.reactor[mo] = f
	return nil
}

func (sw *Switch) Start() error {
	privData, err := base64.StdEncoding.DecodeString(string(sw.cfg.PrivateKey))
	if err != nil {
		return err
	}
	priv, err := crypto.UnmarshalPrivateKey(privData)
	if err != nil {
		return err
	}
	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(sw.cfg.Address),
		libp2p.EnableRelay(circuit.OptHop),
		libp2p.Identity(priv),
		libp2p.Security(secio.ID, secio.New),
	}
	ctx := context.Background()
	host, err := libp2p.New(ctx, opts...)
	if err != nil {
		sw.log.Error("new libp2p host failed @ p2p.Start, err: %v", err)
		return err
	}

	sw.host = host
	sw.host.SetStreamHandler(protocol.ID(protocolPrefix), sw.handleStream)
	// Build host multiaddress
	hostAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/p2p/%s", sw.host.ID().Pretty()))
	addr := sw.host.Addrs()[0]
	fullAddr := addr.Encapsulate(hostAddr)
	sw.id = &fullAddr
	sw.log.Info("new p2pnode @ p2p.Start host's multiaddr: %s", fullAddr)

	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeServer),
		dht.RoutingTableRefreshPeriod(3 * time.Second),
		dht.ProtocolPrefix(protocol.ID(protocolPrefix)),
	}
	if sw.kdht, err = dht.New(ctx, host, dhtOpts...); err != nil {
		sw.log.Error("new dht host failed @ p2p.Start, err: %v", err)
		return err
	}

	if err := sw.bootstrap(ctx); err != nil {
		sw.log.Error("bootstrap failed @ p2p.Start, err: %v", err)
		return err
	}

	sw.acceptRoutine()

	return nil
}

func (sw *Switch) Stop() error {
	defer sw.timer.Stop()

	f := func(peer Peer) bool {
		peer.FlushStop()
		return true
	}
	rchan := sw.peers.Range(f)
	<-rchan

	sw.quit <- struct{}{}
	return nil
}

// Peers

// Broadcast runs a go routine for each attempted send, which will block trying
// to send for defaultSendTimeoutSeconds. Returns a channel which receives
// success values for each attempted send (false if times out). Channel will be
// closed once msg bytes are sent to all peers (or time out).
//
// NOTE: Broadcast uses goroutines, so order of broadcast may not be preserved.
func (sw *Switch) Broadcast(chID int32, msgBytes []byte) {
	f := func(p Peer) bool {
		return p.Send(chID, msgBytes)
	}
	ch := sw.peers.Range(f)
	<-ch

	sw.log.Info("Broadcast completed @ Broadcast, chID: %d, msgBytes: %X", chID, msgBytes)
}

func (sw *Switch) Send(peer string, chID int32, msgBytes []byte) error {
	p, err := sw.peers.Find(PeerID(peer))
	if err != nil {
		sw.log.Error("fail to send @ Send, err: %v, peer_id: %v, chID: %d, msgBytes: %X", err, peer, chID, msgBytes)
		return err
	}
	p.Send(chID, msgBytes)
	return nil
}

func (sw *Switch) GetP2PID(peerID string) (string, error) {
	return peerID, nil
}

func (sw *Switch) genPeerMultiID(id peer.ID) string {
	peer := sw.host.Peerstore().PeerInfo(id)
	return fmt.Sprintf("%s/%s", peer.Addrs[0], peer.ID)
}

func (sw *Switch) bootstrap(ctx context.Context) error {
	if err := sw.kdht.Bootstrap(ctx); err != nil {
		sw.log.Error("kdht bootstrap failed @ p2p.bootstrap, err: %v", err)
		return err
	}

	for _, peerMutltiID := range sw.cfg.BootStrap {
		peerAddr, err := ipfsaddr.ParseString(peerMutltiID)
		if err != nil {
			sw.log.Error("parse string failed @ p2p.bootstrap, peer: %s, err: %v", peerMutltiID, err)
			continue
		}
		addrInfo, err := peer.AddrInfoFromP2pAddr(peerAddr.Multiaddr())
		if err != nil {
			sw.log.Error("add addrinfo failed @ p2p.bootstrap, err: %v", err)
			continue
		}
		if err := sw.host.Connect(ctx, *addrInfo); err != nil {
			sw.log.Error("connect failed @ p2p.bootstrap, err: %v, addrinfo: %+v", err, *addrInfo)
			continue
		}
	}

	for _, peerID := range sw.kdht.RoutingTable().ListPeers() {
		if err := sw.dialPeersAsync(peerID); err != nil {
			sw.log.Error("dail fail @ bootstrap peer_id: %s, err: %v", peerID, err)
		}
		sw.log.Info("build stream success @ bootstrap remote_peer: %s", peerID)
	}

	return nil
}

// DialPeersAsync dials a list of peers asynchronously in random order.
// Used to dial peers from config on startup or from unsafe-RPC (trusted sources).
// It ignores ErrNetAddressLookup. However, if there are other errors, first
// encounter is returned.
// Nop if there are no peers.
func (sw *Switch) dialPeersAsync(id peer.ID) error {
	ctx := context.Background()
	stream, err := sw.host.NewStream(ctx, id, protocol.ID(protocolPrefix))
	if err != nil {
		sw.log.Error("host make newstream fail @ DialPeersAsync, peer_id: %s, err: %v", id.Pretty(), err)
		return err
	}
	rawPeer := sw.host.Peerstore().PeerInfo(id)
	peer, err := NewDefaultPeer(&rawPeer, stream, sw.reactor, sw.log)
	if err != nil {
		sw.log.Error("new remote peer fail @ DialPeersAsync, peer_id: %s, err: %v", id.Pretty(), err)
		return err
	}
	sw.peers.Add(peer)
	peer.Start()
	return nil
}

func (sw *Switch) acceptRoutine() {
	for {
		select {
		case <-sw.timer.C:
			for _, peerID := range sw.kdht.RoutingTable().ListPeers() {
				sw.log.Info("routing table range @ acceptRoutine, peer_multi: %s", sw.genPeerMultiID(peerID))
			}
		case <-sw.quit:
			sw.log.Error("switch meets end @ acceptRoutine, return")
			return
		}
	}
}

func (sw *Switch) handleStream(netStream network.Stream) {
	old, err := sw.peers.Find(PeerID(netStream.Conn().RemotePeer()))
	if err == nil {
		if err := old.Validate(); err == nil {
			sw.log.Error("use an old one @ handleStream, peer_id: %s", netStream.Conn().RemotePeer())
			return
		}
	}
	p := sw.host.Peerstore().PeerInfo(netStream.Conn().RemotePeer())
	peer, err := NewDefaultPeer(&p, netStream, sw.reactor, sw.log)
	if err != nil {
		sw.log.Error("new remote peer fail @ handleStream, peer_id: %s, err: %v", netStream.Conn().RemotePeer(), err)
		return
	}
	sw.peers.Add(peer)
	peer.Start()
	sw.log.Info("build stream success from a new remote peer @ handleStream, peer_id: %s", netStream.Conn().RemotePeer())
}

//---------------------------------------------------------------------------------------------------
type Config struct {
	Address    string
	BootStrap  []string
	PrivateKey []byte // only for networking
	PublicKey  []byte // only for networking

	TickerTimeSec int64
}
