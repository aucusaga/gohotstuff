package p2p

import (
	"net"

	peer "github.com/libp2p/go-libp2p-core/peer"
)

// NodeInfo exposes basic info of a node
// and determines if we're compatible.
type NodeInfo interface {
	ID() PeerID
	NetAddress() (*NetAddress, error)

	// for use in the handshake.
	Validate() error
	CompatibleWith(other NodeInfo) error
}

// NetAddress defines information about a peer on the network
// including its ID, IP address, and port.
type NetAddress struct {
	Name string `json:"id"`
	IP   net.IP `json:"ip"`
	Port uint16 `json:"port"`
}

type DefaultNodeInfo struct {
	addr *peer.AddrInfo
}

func (n *DefaultNodeInfo) ID() PeerID { return PeerID(n.addr.ID.Pretty()) }
func (n *DefaultNodeInfo) NetAddress() (*NetAddress, error) {
	// /ip4/127.0.0.1/tcp/30002
	return nil, nil
}

// for use in the handshake.
func (n *DefaultNodeInfo) Validate() error                     { return nil }
func (n *DefaultNodeInfo) CompatibleWith(other NodeInfo) error { return nil }
