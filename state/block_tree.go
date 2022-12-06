package state

import (
	"bytes"
	"sync"

	"github.com/aucusaga/gohotstuff/libs"
	"github.com/aucusaga/gohotstuff/state/bt"
)

type DeserializeQurumCert func(input []byte) (qc QuorumCert, err error)
type NewQurumCert func(senderID string, sign []byte, round int64, id []byte,
	parentRound int64, parentID []byte) (qc QuorumCert, err error)
type SerializeID func(input []byte) string

// BlockTree is an added storage to achieve consensus in a distributed system,
// main infomation like entries should be stored in different way.
type BlockTree struct {
	root *bt.Node

	high    *bt.Node
	generic *bt.Node
	locked  *bt.Node
	commit  *bt.Node
	tree    *bt.BasicTree
	userID  PeerID

	DeserializeF  DeserializeQurumCert
	NewQurumCertF NewQurumCert
	FF            SerializeID

	mutex sync.RWMutex
	log   libs.Logger

	// TODO: deal with an orphan
}

func NewQCTree(user PeerID, rootRound int64, rootID string, rootValue []byte,
	df DeserializeQurumCert, nf NewQurumCert, log libs.Logger) (*BlockTree, error) {
	bt, nr, err := bt.NewBasicTree(rootRound, rootID, rootValue)
	if err != nil {
		return nil, err
	}
	if df == nil || nf == nil {
		df = DefaultDeserialize
		nf = NewDefaultQuorumCert
	}
	qct := &BlockTree{
		root:    nr,
		high:    nr,
		generic: nr,

		tree:          bt,
		userID:        user,
		DeserializeF:  df,
		NewQurumCertF: nf,
		FF:            libs.F,
		log:           log,
	}
	return qct, nil
}

func (t *BlockTree) GetCurrentHighQC() QuorumCert {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	qc, err := t.DeserializeF(t.high.Value)
	if err != nil {
		t.log.Error("deserial fail @ GetCurrentHighQC, err: %v", err)
		return nil
	}
	return qc
}

func (t *BlockTree) GetCurrentRoot() QuorumCert {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	var value []byte
	if t.commit != nil {
		value = t.commit.Value
	}
	qc, err := t.DeserializeF(value)
	if err != nil {
		t.log.Error("deserial fail @ GetCurrentRoot, err: %v", err)
		return nil
	}
	return qc
}

func (t *BlockTree) GetJustify() ([]byte, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if t.high == nil {
		return nil, libs.ErrValNotFound
	}
	return t.high.Value, nil
}

func (t *BlockTree) ExecuteNInsert(qc QuorumCert) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	// try to insert a qc into the basic tree
	currentRound, currentID, err := qc.Proposal()
	if err != nil {
		t.log.Error("get proposal fail @ ExecuteNInsert, err: %v", err)
		return err
	}
	_, parentID, err := qc.ParentProposal()
	if err != nil {
		t.log.Error("get parent proposal fail @ ExecuteNInsert, err: %v", err)
		return err
	}
	bytesV, err := qc.Serialize()
	if err != nil {
		t.log.Error("serialize fail @ ExecuteNInsert, err: %v", err)
		return err
	}
	block, err := t.tree.Insert(bt.Node{
		Round:     currentRound,
		ID:        t.FF(currentID),
		Value:     bytesV,
		ParentKey: t.FF(parentID),
	})
	// ignore repeat err
	if err != nil {
		t.log.Error("insert fail @ ExecuteNInsert, err: %v", err)
		return err
	}
	parentBlock := block.Parent

	// generate highQC for the first time
	if t.high == nil {
		t.high = parentBlock
		return nil
	}
	if err := t.redirectHighQCWithoutLock(parentBlock); err != nil {
		t.log.Error("redirect fail @ ExecuteNInsert, err: %v", err)
		return err
	}
	return nil
}

func (t *BlockTree) ProcessCommit(key string) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if t.commit == nil {
		t.log.Warn("commit qc is nil")
		return nil
	}
	if t.commit.ID != key {
		t.log.Warn("commit key invalid, want: %s, have: %s", t.commit.ID, key)
		return nil
	}
	if err := t.tree.Reset(t.commit); err != nil {
		t.log.Error("reset commit qc fail, err: %v", err)
		return err
	}

	// TODO: clear voteMap
	return nil
}

func (t *BlockTree) ProcessVote(qc QuorumCert, validators []PeerID) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	_, id, err := qc.Proposal()
	if err != nil {
		t.log.Error("get proposal fail @ ProcessVote, err: %v", err)
		return err
	}
	node, err := t.queryWithoutLock(t.FF(id))
	if err != nil {
		t.log.Error("query qc fail @ ProcessVote, err: %v", err)
		return err
	}
	if err := t.redirectHighQCWithoutLock(node); err != nil {
		t.log.Error("redirect fail @ ProcessVote, err: %v", err)
		return err
	}
	return nil
}

func (t *BlockTree) Search(round int64, id []byte) (*bt.Node, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	node, err := t.queryWithoutLock(t.FF(id))
	if err != nil {
		return nil, err
	}
	if node.Round != round {
		return nil, libs.ErrWrongElement
	}
	return node, nil
}

func (t *BlockTree) queryWithoutLock(target string) (item *bt.Node, err error) {
	node, err := t.tree.LookUp(target)
	if err != nil {
		return nil, err
	}
	if node.Value == nil {
		return nil, libs.ErrWrongElement
	}
	return node, nil
}

// redirect highQC & genericQC & lockedQC & commitQC
func (t *BlockTree) redirectHighQCWithoutLock(b *bt.Node) error {
	highQC, err := t.DeserializeF(t.high.Value)
	if err != nil {
		t.log.Error("deserial fail @ redirectHighQCWithoutLock, err: %v", err)
		return err
	}
	hiRound, hiID, err := highQC.Proposal()
	if err != nil {
		t.log.Error("get proposal fail @ redirectHighQCWithoutLock, err: %v", err)
		return err
	}
	// always refresh a new high node with a diffetent ID
	if hiRound > b.Round ||
		(!bytes.Equal(hiID, []byte(b.ID)) && hiRound == b.Round) {
		t.log.Warn("reset fail @ redirectHighQCWithoutLock, hiRound: %d, inRound: %d, hiID: %s, inID: %s", hiRound, b.Round, libs.F(hiID), b.ID)
		return nil
	}
	t.high = b
	if b.Parent != nil {
		t.generic = b.Parent
	}
	if b.Parent != nil && b.Parent.Parent != nil {
		t.locked = b.Parent.Parent
	}
	if b.Parent != nil && b.Parent.Parent != nil && b.Parent.Parent.Parent != nil {
		t.commit = b.Parent.Parent.Parent
	}

	return nil
}
