# gohotstuff

<img src="https://raw.githubusercontent.com/aucusaga/gohotstuff/main/image/hot_gopher.png" width = "200" height = "200"/>

**Gohotstuff** is a go library which implements the chained-hotStuff consensus protocol, more precisely, the LibraBFT algorithmic core. 

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

