package state

import (
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/aucusaga/gohotstuff/libs"
)

const (
	TypeCollectVotes = 1 // "collect_votes_type"
	TypeNextRound    = 2 // "next_round_type"

	MaxTimeoutSec = 60 * 60
)

var (
	tickTockBufferSize = 10
)

// TimeoutTicker is a timer that schedules timeouts
// conditional on the height/round/step in the timeoutInfo.
type TimeoutTicker interface {
	Start()
	Stop()
	Chan() <-chan timeoutInfo       // on which to receive a timeout
	ScheduleTimeout(ti timeoutInfo) // reset the timer
}

// internally generated messages which may update the state
type timeoutInfo struct {
	Type     int           `json:"type"`
	Round    int64         `json:"round"`
	Index    int64         `json:"index"`
	Duration time.Duration `json:"duration"`
}

type DefaultTimeoutTicker struct {
	timer    *time.Timer
	tickChan chan timeoutInfo // for scheduling timeouts
	tockChan chan timeoutInfo // for notifying about them

	quit chan struct{}
	log  libs.Logger
}

// NewDefaultTimeoutTicker returns a new DefaultTimeoutTicker and invoke timeoutTicker.Start().
func NewDefaultTimeoutTicker(logger libs.Logger) TimeoutTicker {
	if logger == nil {
		logger = logs.NewLogger()
	}
	tt := &DefaultTimeoutTicker{
		timer:    time.NewTimer(MaxTimeoutSec * time.Second),
		tickChan: make(chan timeoutInfo, tickTockBufferSize),
		tockChan: make(chan timeoutInfo, tickTockBufferSize),
		log:      logger,
	}
	return tt
}

func (t *DefaultTimeoutTicker) Start() {
	go t.timeoutRoutine()
}

// ScheduleTimeout schedules a new timeout by sending on the internal tickChan.
// The timeoutRoutine is always available to read from tickChan, so this won't block.
// The scheduling may fail if the timeoutRoutine has already scheduled a timeout for a later height/round/step.
func (t *DefaultTimeoutTicker) ScheduleTimeout(ti timeoutInfo) {
	t.tickChan <- ti
	t.log.Info("new timeout info: %+v", ti)
}

// Chan returns a channel on which timeouts are sent.
func (t *DefaultTimeoutTicker) Chan() <-chan timeoutInfo {
	return t.tockChan
}

func (t *DefaultTimeoutTicker) Stop() {
	defer t.timer.Stop()
	defer close(t.tickChan)
	defer close(t.tockChan)
	t.quit <- struct{}{}
}

// send on tickChan to start a new timer.
// timers are interupted and replaced by new ticks from later steps
// timeouts of 0 on the tickChan will be immediately relayed to the tockChan
func (t *DefaultTimeoutTicker) timeoutRoutine() {
	t.log.Info("Starting timeout routine")
	var ti timeoutInfo
	for {
		select {
		case newti := <-t.tickChan:
			t.log.Info("Received tick, old timeout info: %+v, new_ti: %+v", ti, newti)

			// ignore tickers for old round
			if newti.Round < ti.Round {
				continue
			}

			t.timer.Stop()
			// update timeoutInfo and reset timer
			// NOTE time.Timer allows duration to be non-positive
			t.timer.Reset(newti.Duration)
			ti = newti
		case <-t.timer.C:
			t.log.Info("Timed out, dur: %+v, round: %+v", ti.Duration, ti.Round)
			// go routine here guarantees timeoutRoutine doesn't block.
			// Determinism comes from playback in the receiveRoutine.
			// We can eliminate it by merging the timeoutRoutine into receiveRoutine
			//  and managing the timeouts ourselves with a millisecond ticker
			go func() { t.tockChan <- ti }()
		case <-t.quit:
			return
		}
	}
}
