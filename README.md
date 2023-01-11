# gohotstuff

<img src="https://github.com/aucusaga/gohotstuff/blob/main/image/hot_gopher.png" width = "200" height = "200"/>

**Gohotstuff** is a Go library which implements the chained-hotStuff consensus protocol, more precisely, the LibraBFT algorithmic core. It has been used as a consensus component to help large-scale online blockchain system  functioning normally (See the repo **[Xuperchain](https://github.com/xuperchain/xuperchain)**).


Start
------------------
1. Compiling our project is simple
~~~ shell
	make
~~~
2. Cmds
	
    Dictionary **output** contains bin and configurations.Use command start to run up a single hotstuff node.
~~~ shell
	gohotstuff start
~~~ 
    Gohotstuff uses [libp2p](https://github.com/libp2p/libp2p) to build peer-to-peer systems where the network key should be generated as an communication key, an identity as well. Please generate nodes own network key and crypto key before start-up.
~~~ shell
	gohotstuff genkey --type network
    gohotstuff genkey --type crypto
~~~ 
	Network identity alse follows libp2p style, uses address type to preview the node's identity, the last slash part of which indicates the validator name.
~~~ shell
    // output like: /ip4/127.0.0.1/tcp/30001/p2p/Qmf2HeHe4sspGkfRCTq6257Vm3UHzvh2TeQJHHvHzzuFw6
    // use `/ip4/127.0.0.1/tcp/30001` as the local p2p address
    // use `Qmf2HeHe4sspGkfRCTq6257Vm3UHzvh2TeQJHHvHzzuFw6` as the validator name.
    gohotstuff preview
~~~ 

Configuration
------------------
See the dictionary conf.


Build up a System
-------------------
Use commands mentioned before can specify a new node with the new configuration. Also, we can start up different nodes with different network identities to build up a hotstuff peer-to-peer system.

**Attention:** The bootstrap node should be started at the very begining.

Customization
------------------
Users can definite pacemaker|election|saftyrules objects and register with a new state.
See the initialization of a state machine in our code as an example:
~~~ golang
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
~~~
