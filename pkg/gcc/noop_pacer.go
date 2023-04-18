package gcc

import (
	"errors"
	"fmt"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// ErrUnknownStream is returned when trying to send a packet with a SSRC that
// was never registered with any stream
var ErrUnknownStream = errors.New("unknown ssrc")

// NoOpPacer implements a pacer that always immediately sends incoming packets
type NoOpPacer struct {
	ssrcToWriter sync.Map
}

// NewNoOpPacer initializes a new NoOpPacer
func NewNoOpPacer() *NoOpPacer {
	return &NoOpPacer{}
}

// SetTargetBitrate sets the bitrate at which the pacer sends data. NoOp for
// NoOp pacer.
func (p *NoOpPacer) SetTargetBitrate(int) {
}

// AddStream adds a stream and corresponding writer to the p
func (p *NoOpPacer) AddStream(ssrc uint32, writer interceptor.RTPWriter) {
	p.ssrcToWriter.Store(ssrc, writer)
}

// Write sends a packet with header and payload to a previously added stream
func (p *NoOpPacer) Write(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	if entry, ok := p.ssrcToWriter.Load(header.SSRC); ok {
		return entry.(interceptor.RTPWriter).Write(header, payload, attributes)
	}

	return 0, fmt.Errorf("%w: %v", ErrUnknownStream, header.SSRC)
}

// Close closes p
func (p *NoOpPacer) Close() error {
	return nil
}
