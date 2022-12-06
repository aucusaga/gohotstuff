package state

import (
	"encoding/json"
	"fmt"

	"github.com/aucusaga/gohotstuff/libs"
)

type QuorumCert interface {
	Proposal() (round int64, id []byte, err error) // QC's id shoud be unique.
	ParentProposal() (round int64, id []byte, err error)
	Sender() (senderID string)
	Signatures(peerID string) (signType int, sign []byte, pk []byte, err error)
	Serialize() ([]byte, error)
	String() string
}

// DefaultQuorumCert is the canonical implementation of the QuorumCert interface
type DefaultQuorumCert struct {
	Round       int64  `json:"round"`
	ID          []byte `json:"id"`
	ParentRound int64  `json:"parent_round"`
	ParentID    []byte `json:"parent_id"`
	SenderID    string `json:"sender"`

	Signs map[string]DefaultSign `json:"signs"`
	// lastCommitID []byte
}

func NewDefaultQuorumCert(senderID string, sign []byte,
	round int64, id []byte, parentRound int64, parentID []byte) (QuorumCert, error) {
	qc := DefaultQuorumCert{
		Round:       round,
		ID:          id,
		ParentRound: parentRound,
		ParentID:    parentID,
		SenderID:    senderID,
		Signs:       make(map[string]DefaultSign),
	}
	// self-signed certificate

	return qc, nil
}

func DefaultDeserialize(input []byte) (QuorumCert, error) {
	var qc DefaultQuorumCert
	if input == nil {
		return qc, nil
	}
	if err := json.Unmarshal(input, &qc); err != nil {
		return nil, err
	}
	return qc, nil
}

func (qc DefaultQuorumCert) Proposal() (int64, []byte, error) {
	return qc.Round, qc.ID, nil
}

func (qc DefaultQuorumCert) ParentProposal() (int64, []byte, error) {
	return qc.Round, qc.ParentID, nil
}
func (qc DefaultQuorumCert) Sender() string {
	return qc.SenderID
}

func (qc DefaultQuorumCert) Signatures(peerID string) (int, []byte, []byte, error) {
	sign, ok := qc.Signs[peerID]
	if !ok {
		return -1, nil, nil, libs.ErrValNotFound
	}
	return sign.Type, sign.Sign, sign.PublicKey, nil
}

func (qc DefaultQuorumCert) Serialize() ([]byte, error) {
	return json.Marshal(qc)
}

func (qc DefaultQuorumCert) String() string {
	var signs string
	for peerID, s := range qc.Signs {
		signs += fmt.Sprintf("[peer_id: %s, sign: %s] ", peerID, string(s.Sign))
	}
	basic := fmt.Sprintf("round: %d, id: %s, parent_round: %d, parent_id: %s, from: %s, signs: [%s]", qc.Round,
		libs.F(qc.ID), qc.ParentRound, libs.F(qc.ParentID), qc.SenderID, signs)

	return basic
}

type DefaultSign struct {
	PeerID    string
	PublicKey []byte
	Sign      []byte
	Type      int
}
