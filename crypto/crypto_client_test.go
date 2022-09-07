package crypto

import (
	"testing"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/pb"
)

var (
	priKey = "{\"Curvname\":\"P-256\",\"X\":74695617477160058757747208220371236837474210247114418775262229497812962582435,\"Y\":51348715319124770392993866417088542497927816017012182211244120852620959209571,\"D\":29079635126530934056640915735344231956621504557963207107451663058887647996601}"
)

func TestSignNVerify(t *testing.T) {
	if err := InitCryptoClient([]byte(priKey)); err != nil {
		t.Errorf("init crypto client err, err: %v", err)
		return
	}

	cc := CryptoClientPicker()

	msg := &pb.Message{
		Module: libs.ConsensusModule,
		Sum: &pb.Message_Proposal{
			Proposal: &pb.ProposalMessage{
				Module: libs.ConsensusModule,
				Round:  1,
			},
		},
	}
	b, err := msg.Marshal()
	if err != nil {
		t.Errorf("marshal err, err: %v", err)
		return
	}
	newMSG, err := cc.Sign(b)
	if err != nil {
		t.Errorf("sign err, err: %v", err)
		return
	}
	valid, err := cc.Verify(nil, nil, newMSG)
	if err != nil {
		t.Errorf("verify err, err: %v", err)
		return
	}
	if !valid {
		t.Errorf("sign and verify fail")
	}
}
