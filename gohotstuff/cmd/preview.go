package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/p2p"
	"github.com/spf13/cobra"
)

type AddressCmd struct {
	Cmd *cobra.Command
}

func GetAddressCmd() *AddressCmd {
	cmd := new(AddressCmd)
	cmd.Cmd = &cobra.Command{
		Use:           "preview",
		Short:         "preview the address of the hotstuff node, the last part indicates the validator name.",
		Example:       "gohotstuff address, `/ip4/127.0.0.1/tcp/30001/p2p/Qmf2HeHe4sspGkfRCTq6257Vm3UHzvh2TeQJHHvHzzuFw6`, `Qmf2HeHe4sspGkfRCTq6257Vm3UHzvh2TeQJHHvHzzuFw6` is the validator name",
		SilenceUsage:  true,
		SilenceErrors: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			return PreviewAddress()
		},
	}

	return cmd
}

func PreviewAddress() error {
	cfgPath := KeyDirReady(AddressName)
	conf := filepath.Join(cfgPath, "conf.yaml")
	cfg, err := libs.GetConfig(conf)
	if err != nil {
		return fmt.Errorf("load configuration failed, err: %v", err)
	}
	ipprefix := cfg.Address

	networkPath := KeyDirReady(NetworkName)
	pretty, err := p2p.GetPeerIDFromPath(networkPath)
	if err != nil {
		return err
	}

	fmt.Printf("%s/p2p/%s\n", ipprefix, pretty)
	return nil
}
