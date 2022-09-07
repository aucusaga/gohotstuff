package libs

import (
	"sync"
)

func init() {
	initLog()
}

var _once sync.Once

// Logger is what any go-hotstuff library should take.
type Logger interface {
	Error(msg string, ctx ...interface{})
	Warn(msg string, ctx ...interface{})
	Info(msg string, ctx ...interface{})
	Trace(msg string, ctx ...interface{})
	Debug(msg string, ctx ...interface{})
}

func initLog() {
	_once.Do(func() {
		//logs.InitLog()
	})
}
