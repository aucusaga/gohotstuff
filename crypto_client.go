package goHotStuff

var (
	cryptoClientPicker func(clusterName string) CryptoClient
)

type CryptoClient interface {
	Sign(msgType int64, msgBytes []byte) ([]byte, error)
	Verify(msgType int64, sign []byte, msgBytes []byte) (bool, error)
}

// RegisterCryptoClient registers a crypto client initialization function.
func RegisterCryptoClient(fn func() CryptoClient) {
	if cryptoClientPicker != nil {
		panic("RegisterCryptoClient called more than once")
	}
	cryptoClientPicker = func(clusterName string) CryptoClient {
		return fn()
	}
}
