package node

import (
	"io/ioutil"
	"path/filepath"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/crypto"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/p2p"
	"github.com/aucusaga/gohotstuff/state"
)

// node is the canonical implementation of the replica
type Node struct {
	cfg *NodeConfig

	p2p *p2p.Switch
	smr *state.State
	cc  crypto.CryptoClient
	// block storage
}

func createConsensus(name string, cc crypto.CryptoClient, cfg *state.ConsensusConfig, logger libs.Logger) (*state.State, error) {
	// ticker is a timer that schedules timeouts conditional on the height/round/step in the timeoutInfo.
	ticker := state.NewDefaultTimeoutTicker(logger)

	smr, err := state.NewState(state.PeerID(name), cc, ticker, logger, cfg)
	if err != nil {
		return nil, err
	}
	// pacemaker controls the current status of the state machine.
	pacemaker := state.NewDefaultPacemaker(cfg.StartRound)
	if err := smr.RegisterPaceMaker(pacemaker); err != nil {
		return nil, err
	}
	// election chooses the specific leader and validators in different round.
	election := state.NewDefaultElection(cfg.StartRound, cfg.StartValidators)
	if err := smr.RegisterElection(election); err != nil {
		return nil, err
	}
	// safetyrules take responsibility for the access control of the procedures.
	safetyRules := state.NewDefaultSafetyRules(smr)
	if err := smr.RegisterSaftyrules(safetyRules); err != nil {
		return nil, err
	}

	return smr, nil
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

func NewNode(config *libs.Config) (*Node, error) {
	logger := logs.NewLogger()

	// load netkeys
	netPath := filepath.Join(filepath.Join(libs.GetCurRootDir(), "conf"), config.Netpath)
	netPriKey, err := ioutil.ReadFile(filepath.Join(netPath, "private.key"))
	if err != nil {
		logger.Warn("load private key err, err: %+v", err)
		panic("cannot get private key")
	}

	// TODO: loading WAL instead of configuration
	var startValidators []state.PeerID
	for _, v := range config.Validators {
		startValidators = append(startValidators, state.PeerID(v))
	}
	cfg := &NodeConfig{
		name: config.Host,
		p2p: &p2p.Config{
			BootStrap:  config.Bootstrap,
			Address:    config.Address,
			PrivateKey: string(netPriKey),
		},
		state: &state.ConsensusConfig{
			StartRound:      int64(config.Round),
			StartID:         config.Startk,
			StartValue:      []byte(config.Startv),
			StartValidators: startValidators,
		},
	}

	// load crypto keys
	keypath := filepath.Join(filepath.Join(libs.GetCurRootDir(), "conf"), config.Keypath)
	priKey, err := ioutil.ReadFile(filepath.Join(keypath, "private.key"))
	if err != nil {
		logger.Warn("load private key err, err: %+v", err)
		panic("cannot get private key")
	}

	err = crypto.InitCryptoClient(priKey)
	if err != nil {
		logger.Warn("init crypto client err, err: %+v", err)
		panic("init crypto client failed")
	}
	cc := crypto.CryptoClientPicker()

	cons, err := createConsensus(cfg.name, cc, cfg.state, logger)
	if err != nil {
		logger.Warn("create consensus err, err: %+v", err)
		return nil, err
	}

	sw, err := createP2P(cfg.p2p, cons, logger)
	if err != nil {
		logger.Warn("create p2p err, err: %+v", err)
		return nil, err
	}

	return &Node{
		cfg: cfg,
		p2p: sw,
		smr: cons,
		cc:  cc,
	}, nil
}

func (n *Node) Start() {
	go n.p2p.Start()
	go n.smr.Start()
}

//-----------------------------------------
type NodeConfig struct {
	name  string
	p2p   *p2p.Config
	state *state.ConsensusConfig
}
