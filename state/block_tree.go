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

// BlockTree is an added storage to achieve consensus in a distributed system,
// main infomation like entries should be stored in different way.
type BlockTree struct {
	root *bt.Node

	high          *bt.Node
	generic       *bt.Node
	locked        *bt.Node
	commit        *bt.Node
	tree          *bt.BasicTree
	userID        PeerID
	deserializeF  DeserializeQurumCert
	newQurumCertF NewQurumCert

	mutex sync.RWMutex

	// TODO: deal with an orphan
}

func NewQCTree(user PeerID, rootRound int64, rootID string, rootValue []byte,
	df DeserializeQurumCert, nf NewQurumCert) (*BlockTree, error) {
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
		deserializeF:  df,
		newQurumCertF: nf,
	}
	return qct, nil
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
		return err
	}
	_, parentID, err := qc.ParentProposal()
	if err != nil {
		return err
	}
	bytesV, err := qc.Serialize()
	if err != nil {
		return err
	}
	block, err := t.tree.Insert(bt.Node{
		Round:     currentRound,
		ID:        string(currentID),
		Value:     bytesV,
		ParentKey: string(parentID),
	})
	// ignore repeat err
	if err != nil && err != libs.ErrRepeatInsert {
		return err
	}
	parentBlock := block.Parent
	// generate highQC for the first time
	if t.high == nil {
		t.high = parentBlock
		return nil
	}
	if err := t.redirectHighQCWithoutLock(parentBlock); err != nil {
		return err
	}
	return nil
}

func (t *BlockTree) ProcessCommit(key string) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	if t.commit == nil {
		return nil
	}
	if t.commit.ID != key {
		return nil
	}
	if err := t.tree.Reset(t.commit); err != nil {
		return err
	}
	t.commit = nil

	// TODO: clear voteMap
	return nil
}

func (t *BlockTree) ProcessVote(qc QuorumCert, validators []PeerID) error {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	_, id, err := qc.Proposal()
	if err != nil {
		return err
	}
	node, err := t.queryWithoutLock(string(id))
	if err != nil {
		return err
	}
	if err := t.redirectHighQCWithoutLock(node); err != nil {
		return err
	}
	return nil
}

func (t *BlockTree) Search(round int64, id []byte) (*bt.Node, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	node, err := t.queryWithoutLock(string(id))
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
	highQC, err := t.deserializeF(t.high.Value)
	if err != nil {
		return err
	}
	hiRound, hiID, err := highQC.Proposal()
	if err != nil {
		return err
	}
	// always refresh a new high node with a diffetent ID
	if hiRound > b.Round ||
		(!bytes.Equal(hiID, []byte(b.ID)) && hiRound == b.Round) {
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
