package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/pb"
	"github.com/golang/protobuf/proto"
)

var (
	CryptoClientPicker func() CryptoClient
)

type CryptoClient interface {
	Sign(msgBytes []byte) (newmsg []byte, err error)
	Verify(sign []byte, pk []byte, msgBytes []byte) (bool, error)
}

// RegisterCryptoClient registers a crypto client initialization function.
func RegisterCryptoClient(fn func() CryptoClient) {
	if CryptoClientPicker != nil {
		panic("RegisterCryptoClient called more than once")
	}
	CryptoClientPicker = func() CryptoClient {
		return fn()
	}
}

// DefaultCryptoClient is a simple crypto client uses NIST P256.
type DefaultCryptoClient struct {
	SK *ecdsa.PrivateKey
	PK *ecdsa.PublicKey
}

type Sign struct {
	R, S *big.Int
}

type Private struct {
	X, Y, D *big.Int
}

func InitCryptoClient(privateBytes []byte) error {
	privateKey := new(Private)
	if err := json.Unmarshal(privateBytes, privateKey); err != nil {
		return err
	}
	pk := ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     privateKey.X,
		Y:     privateKey.Y,
	}
	SK := &ecdsa.PrivateKey{
		PublicKey: pk,
		D:         privateKey.D,
	}
	PK := &pk

	CryptoClientPicker = func() CryptoClient {
		return &DefaultCryptoClient{
			SK: SK,
			PK: PK,
		}
	}
	return nil
}

// Sign uses hotstuffpb only.
// crypto_client will load its own public_key and
// use private_key to sign msg ignore the tag signatrue
func (cc *DefaultCryptoClient) Sign(msgBytes []byte) ([]byte, error) {
	var msg pb.Message
	if err := proto.Unmarshal(msgBytes, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal bytes fail @ crypto.Sign, err: %v", err)
	}

	var new pb.Message
	switch msg := msg.Sum.(type) {
	case *pb.Message_Proposal:
		proposal := &pb.ProposalMessage{
			Module:    libs.ConsensusModule,
			Round:     msg.Proposal.Round,
			Id:        msg.Proposal.Id,
			Timestamp: msg.Proposal.Timestamp,
			Pid:       msg.Proposal.Pid,
			Justify:   msg.Proposal.Justify,
			Pk:        elliptic.Marshal(elliptic.P256(), cc.PK.X, cc.PK.Y),
		}
		wait, err := json.Marshal(proposal)
		if err != nil {
			return nil, err
		}
		signatrue, err := cc.sign(wait)
		if err != nil {
			return nil, err
		}
		proposal.Signature = signatrue

		new.Module = libs.ConsensusModule
		new.Sum = &pb.Message_Proposal{
			Proposal: proposal,
		}
		return proto.Marshal(&new)
	case *pb.Message_Vote:
		vote := &pb.VoteMessage{
			Module:     libs.ConsensusModule,
			VoteInfo:   msg.Vote.VoteInfo,
			CommitInfo: msg.Vote.CommitInfo,
			Timestamp:  msg.Vote.Timestamp,
			Pid:        msg.Vote.Pid,
			Pk:         elliptic.Marshal(elliptic.P256(), cc.PK.X, cc.PK.Y),
		}
		wait, err := json.Marshal(vote)
		if err != nil {
			return nil, err
		}
		signatrue, err := cc.sign(wait)
		if err != nil {
			return nil, err
		}
		vote.Signature = signatrue

		new.Module = libs.ConsensusModule
		new.Sum = &pb.Message_Vote{
			Vote: vote,
		}
		return proto.Marshal(&new)
	case *pb.Message_Timeout:
		timeout := &pb.TimoutMessage{
			Module:      libs.ConsensusModule,
			Round:       msg.Timeout.Round,
			ParentRound: msg.Timeout.ParentRound,
			ParentId:    msg.Timeout.ParentId,
			Index:       msg.Timeout.Index,
			Timestamp:   msg.Timeout.Timestamp,
			Pid:         msg.Timeout.Pid,
			Pk:          elliptic.Marshal(elliptic.P256(), cc.PK.X, cc.PK.Y),
		}
		wait, err := json.Marshal(timeout)
		if err != nil {
			return nil, err
		}
		signatrue, err := cc.sign(wait)
		if err != nil {
			return nil, err
		}
		timeout.Signature = signatrue

		new.Module = libs.ConsensusModule
		new.Sum = &pb.Message_Timeout{
			Timeout: timeout,
		}
		return proto.Marshal(&new)
	default:
	}
	return nil, fmt.Errorf("unknown msg_info type")
}

func (cc *DefaultCryptoClient) Verify(sign []byte, pk []byte, msgBytes []byte) (bool, error) {
	var msg pb.Message
	if err := proto.Unmarshal(msgBytes, &msg); err != nil {
		return false, fmt.Errorf("unmarshal bytes fail @ crypto.Verify, err: %v", err)
	}

	switch msg := msg.Sum.(type) {
	case *pb.Message_Proposal:
		proposal := &pb.ProposalMessage{
			Module:    libs.ConsensusModule,
			Round:     msg.Proposal.Round,
			Id:        msg.Proposal.Id,
			Timestamp: msg.Proposal.Timestamp,
			Pid:       msg.Proposal.Pid,
			Justify:   msg.Proposal.Justify,
			Pk:        msg.Proposal.Pk,
		}
		data, err := json.Marshal(proposal)
		if err != nil {
			return false, err
		}
		return cc.verify(data, msg.Proposal.Signature, msg.Proposal.Pk)
	case *pb.Message_Vote:
		vote := &pb.VoteMessage{
			Module:     libs.ConsensusModule,
			VoteInfo:   msg.Vote.VoteInfo,
			CommitInfo: msg.Vote.CommitInfo,
			Timestamp:  msg.Vote.Timestamp,
			Pid:        msg.Vote.Pid,
			Pk:         msg.Vote.Pk,
		}
		data, err := json.Marshal(vote)
		if err != nil {
			return false, err
		}
		return cc.verify(data, msg.Vote.Signature, msg.Vote.Pk)
	case *pb.Message_Timeout:
		timeout := &pb.TimoutMessage{
			Module:      libs.ConsensusModule,
			Round:       msg.Timeout.Round,
			ParentRound: msg.Timeout.ParentRound,
			ParentId:    msg.Timeout.ParentId,
			Index:       msg.Timeout.Index,
			Timestamp:   msg.Timeout.Timestamp,
			Pid:         msg.Timeout.Pid,
			Pk:          msg.Timeout.Pk,
		}
		data, err := json.Marshal(timeout)
		if err != nil {
			return false, err
		}
		return cc.verify(data, msg.Timeout.Signature, msg.Timeout.Pk)
	default:
	}
	return false, fmt.Errorf("unknown msg_info type")
}

func (cc *DefaultCryptoClient) hash(msgBytes []byte) []byte {
	h := sha256.New()
	h.Write(msgBytes)
	s1 := h.Sum(nil)

	h = sha256.New()
	h.Write(s1)
	return h.Sum(nil)
}

func (cc *DefaultCryptoClient) sign(msgBytes []byte) ([]byte, error) {
	r, s, err := ecdsa.Sign(rand.Reader, cc.SK, []byte(cc.hash(msgBytes)))
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(Sign{r, s})
}

func (cc *DefaultCryptoClient) verify(data, sign []byte, pub []byte) (bool, error) {
	x, y := elliptic.Unmarshal(elliptic.P256(), pub)
	pk := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}

	sig := new(Sign)
	_, err := asn1.Unmarshal(sign, sig)
	if err != nil {
		return false, nil
	}
	return ecdsa.Verify(pk, cc.hash(data), sig.R, sig.S), nil
}
