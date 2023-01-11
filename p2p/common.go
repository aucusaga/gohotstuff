package p2p

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/ioutil"
	"path/filepath"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
)

func GenerateKeyPairWithPath(path string) error {
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	if err != nil {
		return err
	}

	if len(path) == 0 {
		return errors.New("p2p key path empty")
	}

	privData, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Join(path, "private.key"), []byte(base64.StdEncoding.EncodeToString(privData)), 0700)
}

func GetKeyPairFromPath(path string) (crypto.PrivKey, error) {
	if len(path) <= 0 {
		return nil, errors.New("p2p key path empty")
	}
	f := filepath.Join(path, "private.key")
	if !libs.FileIsExist(f) {
		return nil, errors.New("invalid p2p key path")
	}
	data, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}

	privData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, err
	}
	return crypto.UnmarshalPrivateKey(privData)
}

func GetPeerIDFromPath(path string) (string, error) {
	sk, err := GetKeyPairFromPath(path)
	if err != nil {
		return "", err
	}

	pid, err := peer.IDFromPrivateKey(sk)
	if err != nil {
		return "", err
	}
	return pid.Pretty(), nil
}
