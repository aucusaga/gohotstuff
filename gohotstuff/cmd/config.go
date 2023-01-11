package cmd

import (
	"errors"
	"path/filepath"

	"github.com/aucusaga/gohotstuff/crypto"
	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/p2p"
	"github.com/spf13/cobra"
)

const (
	NetworkName = "network"
	CryptoName  = "crypto"
	AddressName = "address"
)

type KeyCmd struct {
	Cmd *cobra.Command
}

func GetKeyCmd() *KeyCmd {
	cmd := new(KeyCmd)
	var keyType string

	cmd.Cmd = &cobra.Command{
		Use:           "genkey",
		Short:         "--type network|crypto|address, generate keys for the hotstuff node.",
		Example:       "gohotstuff genkey --type network | crypto | address",
		SilenceUsage:  true,
		SilenceErrors: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			return GenerateKeys(keyType)
		},
	}

	cmd.Cmd.Flags().StringVarP(&keyType, "type", "t", "",
		"key's type")

	return cmd
}

func GenerateKeys(key string) error {
	switch key {
	case NetworkName:
		return GenerateNetworkKey()
	case CryptoName:
		return GenerateCryptoKey()
	default:
		return errors.New("key's type invalid, must be `network` or `crypto`")
	}
}

func GenerateNetworkKey() error {
	cfgPath := KeyDirReady(NetworkName)
	if err := p2p.GenerateKeyPairWithPath(cfgPath); err != nil {
		return errors.New("gen network key fail")
	}
	return nil
}

func GenerateCryptoKey() error {
	cfgPath := KeyDirReady(CryptoName)
	if err := crypto.GenKeyPair(cfgPath); err != nil {
		return errors.New("gen crypto key fail")
	}
	return nil
}

func KeyDirReady(key string) string {
	var cfgPath string
	switch key {
	case NetworkName:
		cfgPath = filepath.Join(libs.GetCurRootDir(), "output/conf/netkeys")
	case CryptoName:
		cfgPath = filepath.Join(libs.GetCurRootDir(), "output/conf/keys")
	case AddressName:
		cfgPath = filepath.Join(libs.GetCurRootDir(), "output/conf")
	default:
		panic("invalid key type, must be `network` or `crypto`")
	}

	if !libs.FileIsExist(cfgPath) {
		if err := libs.MakeDir(cfgPath); err != nil {
			panic("mkdir key dir fail, /output/${CONF} or /output/${CONF}/keys or /output/${CONF}/netkeys doesn's exist.")
		}
	}

	return cfgPath
}
