// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package gcc

import (
	"errors"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

var errLeakyBucketPacerPoolCastFailed = errors.New("failed to access leaky bucket pacer pool, cast failed")
var errLeakyBucketPacerQueueItemsPoolCastFailed = errors.New("failed to access leaky bucket pacer queue items pool, cast failed")
var errLeakyBucketPacerQueueFull = errors.New("failed to add item to queue: channel is full")

const pacerQueueMaxSize = 10000

type item struct {
	header     *rtp.Header
	payload    []byte
	size       int
	attributes interceptor.Attributes
}

// LeakyBucketPacer implements a leaky bucket pacing algorithm
type LeakyBucketPacer struct {
	log logging.LeveledLogger

	f                 float64
	targetBitrate     int
	targetBitrateLock sync.RWMutex

	queue       chan *item
	done        chan struct{}
	tickCh      chan time.Time
	disableCopy bool

	ssrcToWriter sync.Map
}

var pacerBufPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 1460)
	},
}

var pacerItemsPool = &sync.Pool{
	New: func() interface{} {
		return &item{}
	},
}

// PacerOption configures a pacer
type PacerOption func(*LeakyBucketPacer) error

// PacerDisableCopy bypasses copy of underlying packets.
// It should be used when you are not re-using underlying buffers of packets that have been written
func PacerDisableCopy() PacerOption {
	return func(p *LeakyBucketPacer) error {
		p.disableCopy = true
		return nil
	}
}

// NewLeakyBucketPacer initializes a new LeakyBucketPacer
func NewLeakyBucketPacer(initialBitrate int, opts ...PacerOption) (*LeakyBucketPacer, error) {
	p := &LeakyBucketPacer{
		log:           logging.NewDefaultLoggerFactory().NewLogger("pacer"),
		f:             1.5,
		targetBitrate: initialBitrate,
		queue:         make(chan *item, pacerQueueMaxSize),
		done:          make(chan struct{}),
		tickCh:        make(chan time.Time),
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	ticker.AddChannel(p.tickCh)

	go p.Run()
	return p, nil
}

// AddStream adds a new stream and its corresponding writer to the pacer
func (p *LeakyBucketPacer) AddStream(ssrc uint32, writer interceptor.RTPWriter) {
	p.ssrcToWriter.Store(ssrc, writer)
}

// SetTargetBitrate updates the target bitrate at which the pacer is allowed to
// send packets. The pacer may exceed this limit by p.f
func (p *LeakyBucketPacer) SetTargetBitrate(rate int) {
	p.targetBitrateLock.Lock()
	defer p.targetBitrateLock.Unlock()
	p.targetBitrate = int(p.f * float64(rate))
}

func (p *LeakyBucketPacer) getTargetBitrate() int {
	p.targetBitrateLock.RLock()
	defer p.targetBitrateLock.RUnlock()

	return p.targetBitrate
}

// Write sends a packet with header and payload the a previously registered
// stream.
func (p *LeakyBucketPacer) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	var buf []byte
	if p.disableCopy {
		buf = payload
	} else {
		var ok bool
		buf, ok = pacerBufPool.Get().([]byte)
		if !ok {
			return 0, errLeakyBucketPacerPoolCastFailed
		}
		copy(buf, payload)
	}

	qItem, ok := pacerItemsPool.Get().(*item)
	if !ok {
		return 0, errLeakyBucketPacerQueueItemsPoolCastFailed
	}

	hdrCopy := header.Clone()
	qItem.header = &hdrCopy
	qItem.payload = buf
	qItem.size = len(payload)
	qItem.attributes = attributes

	select {
	case p.queue <- qItem:
	default:
		return 0, errLeakyBucketPacerQueueFull
	}

	return header.MarshalSize() + len(payload), nil
}

// Run starts the LeakyBucketPacer
func (p *LeakyBucketPacer) Run() {
	lastSent := time.Now()
	for {
		select {
		case <-p.done:
			return
		case now := <-p.tickCh:
			if len(p.queue) == 0 {
				continue
			}

			budget := int(float64(now.Sub(lastSent).Milliseconds()) * float64(p.getTargetBitrate()) / 8000.0)

			for len(p.queue) != 0 && budget > 0 {
				//p.log.Infof("budget=%v, len(queue)=%v, targetBitrate=%v", budget, len(p.queue), p.getTargetBitrate())

				next, ok := <-p.queue

				entry, ok := p.ssrcToWriter.Load(next.header.SSRC)
				if !ok {
					p.log.Warnf("no writer found for ssrc: %v", next.header.SSRC)
					pacerItemsPool.Put(next)
					if !p.disableCopy {
						pacerBufPool.Put(next.payload)
					}
					continue
				}
				writer := entry.(interceptor.RTPWriter)

				if next.attributes == nil {
					next.attributes = make(interceptor.Attributes)
				}
				next.attributes.Set(timeNowAttributesKey, &now)

				n, err := writer.Write(next.header, (next.payload)[:next.size], next.attributes)
				if err != nil {
					p.log.Errorf("failed to write packet: %v", err)
				}
				lastSent = now
				budget -= n

				if !p.disableCopy {
					pacerBufPool.Put(next.payload)
				}
				pacerItemsPool.Put(next)
			}
		}
	}
}

// Close closes the LeakyBucketPacer
func (p *LeakyBucketPacer) Close() error {
	close(p.done)
	return ticker.RemoveChannel(p.tickCh)
}
