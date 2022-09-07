package libs

import (
	"errors"
)

var (
	ErrValNotFound     = errors.New("cannot find target, it's a nil pointer or an empty value struct")
	ErrWrongElement    = errors.New("element type invalid, check parameters")
	ErrParentEmptyNode = errors.New("node's parent is empty")
	ErrOrphanNode      = errors.New("cannot find the location where the node can be inserted")
	ErrRepeatInsert    = errors.New("key has been inserted before")
)
