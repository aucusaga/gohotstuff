package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

func GenKeyPair(path string) error {
	priKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}

	newPri := Private{
		X:    priKey.X,
		Y:    priKey.Y,
		D:    priKey.D,
		Curvname: elliptic.P256().Params().Name,
	}
	ecPrivateKey, e := json.Marshal(newPri)
	if e != nil {
		return e
	}

	return ioutil.WriteFile(filepath.Join(path, "private.key"), ecPrivateKey, 0700)
}
