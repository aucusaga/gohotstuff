package bt

import (
	"sync"

	"github.com/aucusaga/gohotstuff/libs"
)

// Tree holds elements of a single tree.
// Element's key should be unique.
type BasicTree struct {
	root   *Node
	keySet map[string]struct{}

	mutex sync.RWMutex
}

func NewBasicTree(rootRound int64, rootID string, rootValue []byte) (*BasicTree, *Node, error) {
	if rootRound < 0 || rootID == "" || rootValue == nil {
		return nil, nil, libs.ErrWrongElement
	}
	newR := Node{
		Round: rootRound,
		ID:    rootID,
		Value: rootValue,
	}
	bt := BasicTree{
		root:   &newR,
		keySet: make(map[string]struct{}),
	}
	return &bt, bt.root, nil
}

// Insert inserts a node into the tree, parent of the node shoule be indicated.
func (t *BasicTree) Insert(n Node) (*Node, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if n.Round < 0 || n.ID == "" || n.Value == nil {
		return nil, libs.ErrWrongElement
	}
	if _, ok := t.keySet[n.ID]; ok {
		return nil, libs.ErrRepeatInsert
	}
	insertedNode := Node{
		ID:        n.ID,
		Value:     n.Value,
		ParentKey: n.ParentKey,
	}
	// for initial tree
	if t.root == nil {
		t.root = &insertedNode
		return nil, nil
	}
	if n.ParentKey == "" {
		return nil, libs.ErrParentEmptyNode
	}
	parent, err := t.lookup(t.root, n.ParentKey)
	if err != nil {
		return nil, libs.ErrOrphanNode
	}
	if parent.Round >= n.Round {
		return nil, libs.ErrWrongElement
	}
	t.keySet[n.ID] = struct{}{}
	insertedNode.Parent = parent
	parent.Sons = append(parent.Sons, &insertedNode)
	return parent, nil
}

// Reset sets the input as a new root.
func (t *BasicTree) Reset(newRoot *Node) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if newRoot.Round < 0 || newRoot.ID == "" || newRoot.Value == nil {
		return libs.ErrWrongElement
	}
	if newRoot.Parent != nil {
		t.delete(t.root, newRoot.ID)
		newRoot.Parent.Sons = nil
		newRoot.Parent = nil
	}
	t.root = newRoot
	return nil
}

// LookUp finds the specific node holds the key.
func (t *BasicTree) LookUp(key string) (*Node, error) {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	return t.lookup(t.root, key)
}

// lookup implements a simple dfs searching algorithm.
func (t *BasicTree) lookup(n *Node, key string) (*Node, error) {
	if key == "" || n == nil {
		return nil, libs.ErrWrongElement
	}
	if n.ID == key {
		return n, nil
	}
	if n.Sons == nil {
		return nil, libs.ErrValNotFound
	}
	for _, son := range n.Sons {
		if n, _ := t.lookup(son, key); n != nil {
			return n, nil
		}
	}
	return nil, libs.ErrValNotFound
}

func (t *BasicTree) delete(begin *Node, target string) {
	if begin.ID == target {
		return
	}
	if len(begin.Sons) == 0 {
		return
	}
	delete(t.keySet, begin.ID)
	for _, son := range begin.Sons {
		t.delete(son, target)
	}
}

// Node is a single element within the tree
type Node struct {
	Round     int64
	ID        string
	Value     []byte
	ParentKey string

	Parent *Node
	Sons   []*Node
}
