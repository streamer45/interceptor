package gcc

import (
	"math"
	"sync"
	"time"

	"github.com/pion/logging"
)

const (
	// constants from
	// https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02#section-6

	increaseLossThreshold = 0.02
	increaseTimeThreshold = 200 * time.Millisecond
	increaseFactor        = 1.05

	decreaseLossThreshold = 0.1
	decreaseTimeThreshold = 200 * time.Millisecond
)

// LossStats contains internal statistics of the loss based controller
type LossStats struct {
	TargetBitrate int
	AverageLoss   float64
}

type lossControllerConfig struct {
	initialBitrate int
	minBitrate     int
	maxBitrate     int
}

type lossBasedBandwidthEstimator struct {
	lock           sync.Mutex
	maxBitrate     int
	minBitrate     int
	bitrate        int
	averageLoss    float64
	lastLossUpdate time.Time
	lastIncrease   time.Time
	lastDecrease   time.Time
	log            logging.LeveledLogger
}

func newLossBasedBWE(c lossControllerConfig) *lossBasedBandwidthEstimator {
	return &lossBasedBandwidthEstimator{
		lock:           sync.Mutex{},
		maxBitrate:     c.maxBitrate,
		minBitrate:     c.minBitrate,
		bitrate:        c.initialBitrate,
		averageLoss:    0,
		lastLossUpdate: time.Time{},
		lastIncrease:   time.Time{},
		lastDecrease:   time.Time{},
		log:            logging.NewDefaultLoggerFactory().NewLogger("gcc_loss_controller"),
	}
}

func (e *lossBasedBandwidthEstimator) getEstimate(wantedRate int) LossStats {
	e.lock.Lock()
	defer e.lock.Unlock()

	if e.bitrate <= 0 {
		e.bitrate = clampInt(wantedRate, e.minBitrate, e.maxBitrate)
	}

	return LossStats{
		TargetBitrate: e.bitrate,
		AverageLoss:   e.averageLoss,
	}
}

func (e *lossBasedBandwidthEstimator) updateLossEstimate(now time.Time, lossRatio float64) {
	e.lock.Lock()
	defer e.lock.Unlock()

	e.averageLoss = e.average(now.Sub(e.lastLossUpdate), e.averageLoss, lossRatio)
	e.lastLossUpdate = now

	increaseLoss := math.Max(e.averageLoss, lossRatio)
	decreaseLoss := math.Min(e.averageLoss, lossRatio)

	if increaseLoss < increaseLossThreshold && now.Sub(e.lastIncrease) > increaseTimeThreshold {
		e.log.Infof("loss controller increasing; averageLoss: %v, decreaseLoss: %v, increaseLoss: %v", e.averageLoss, decreaseLoss, increaseLoss)
		e.lastIncrease = now
		e.bitrate = clampInt(int(increaseFactor*float64(e.bitrate)), e.minBitrate, e.maxBitrate)
	} else if decreaseLoss > decreaseLossThreshold && now.Sub(e.lastDecrease) > decreaseTimeThreshold {
		e.log.Infof("loss controller decreasing; averageLoss: %v, decreaseLoss: %v, increaseLoss: %v", e.averageLoss, decreaseLoss, increaseLoss)
		e.lastDecrease = now
		e.bitrate = clampInt(int(float64(e.bitrate)*(1-0.5*decreaseLoss)), e.minBitrate, e.maxBitrate)
	}
}

func (e *lossBasedBandwidthEstimator) average(delta time.Duration, prev, sample float64) float64 {
	return sample + math.Exp(-float64(delta.Milliseconds())/200.0)*(prev-sample)
}

func (e *lossBasedBandwidthEstimator) setTargetBitrate(rate int) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.bitrate = rate
}

func (e *lossBasedBandwidthEstimator) setMinBitrate(rate int) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.minBitrate = rate
}

func (e *lossBasedBandwidthEstimator) setMaxBitrate(rate int) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.maxBitrate = rate
}
