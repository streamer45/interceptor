package gcc

import (
	"errors"
	"sync"
	"time"

	"github.com/pion/logging"
)

var ticker *globalTicker

const pacingInterval = 5 * time.Millisecond

type globalTicker struct {
	log logging.LeveledLogger

	ticker   *time.Ticker
	channels []chan<- time.Time
	closeCh  chan struct{}

	mut sync.RWMutex
}

func init() {
	ticker = &globalTicker{
		log: logging.NewDefaultLoggerFactory().NewLogger("global_ticker"),
	}
}

func (t *globalTicker) AddChannel(ch chan<- time.Time) {
	t.mut.Lock()
	defer t.mut.Unlock()

	t.channels = append(t.channels, ch)

	if len(t.channels) == 1 {
		t.log.Infof("initializing global ticker")
		t.ticker = time.NewTicker(pacingInterval)
		t.closeCh = make(chan struct{})
		go t.run()
	}
}

func (t *globalTicker) RemoveChannel(tickCh chan<- time.Time) error {
	ticker.mut.Lock()
	defer ticker.mut.Unlock()

	idx := -1
	for i, ch := range ticker.channels {
		if ch == tickCh {
			idx = i
			break
		}
	}

	if idx == -1 {
		return errors.New("failed to find tick channel")
	}

	t.channels[idx] = t.channels[len(t.channels)-1]
	t.channels = t.channels[:len(t.channels)-1]

	if len(ticker.channels) == 0 {
		close(ticker.closeCh)
	}

	return nil
}

func (t *globalTicker) run() {
	for {
		select {
		case now := <-t.ticker.C:
			t.mut.RLock()
			for _, ch := range t.channels {
				select {
				case ch <- now:
				default:
				}
			}
			t.mut.RUnlock()
		case <-t.closeCh:
			t.log.Infof("deinitializing global ticker")
			t.ticker.Stop()
			return
		}
	}
}
