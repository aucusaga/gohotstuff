package node

import (
	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/crypto"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/p2p"
	"github.com/aucusaga/gohotstuff/state"
)

// node is the canonical implementation of the replica
type Node struct {
	cfg *Config

	p2p *p2p.Switch
	smr *state.State
	cc  crypto.CryptoClient
	// block storage
}

func createConsensus(name string, cc crypto.CryptoClient, cfg *state.ConsensusConfig, logger libs.Logger) (*state.State, error) {
	ticker := state.NewDefaultTimeoutTicker(logger)
	sm, err := state.NewState(state.PeerID(name), cc, ticker, logger, cfg)
	if err != nil {
		return nil, err
	}
	return sm, nil
}

func createP2P(cfg *p2p.Config, consensusReactor libs.Reactor, logger libs.Logger) (*p2p.Switch, error) {
	sw, err := p2p.NewSwitch(cfg, logger)
	if err != nil {
		return nil, err
	}
	if err := sw.AddReactor(libs.ConsensusModule, consensusReactor); err != nil {
		return nil, err
	}
	consensusReactor.SetSwitch(sw)
	return sw, nil
}

func NewNode(config *Config) (*Node, error) {
	logger := logs.NewLogger()

	cc := crypto.CryptoClientPicker()

	cons, err := createConsensus(config.name, cc, config.state, logger)
	if err != nil {
		return nil, err
	}

	sw, err := createP2P(config.p2p, cons, logger)
	if err != nil {
		return nil, err
	}

	return &Node{
		cfg: config,
		p2p: sw,
		smr: cons,
		cc:  cc,
	}, nil
}

func (n *Node) Start() {

}

//-----------------------------------------
type Config struct {
	name  string
	p2p   *p2p.Config
	state *state.ConsensusConfig
}
