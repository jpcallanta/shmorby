package navigation

import "time"

// ScrollAcceleration provides variable scroll speed based on gesture frequency.
type ScrollAcceleration struct {
	enabled     bool
	speed       float64
	lastTick    time.Time
	accumulated float64
}

// NewScrollAcceleration creates a new acceleration tracker.
func NewScrollAcceleration() *ScrollAcceleration {
	return &ScrollAcceleration{
		enabled:  false,
		speed:    1.0,
		lastTick: time.Now(),
	}
}

// Enable turns acceleration on or off.
func (a *ScrollAcceleration) Enable(on bool) {
	a.enabled = on
	if !on {
		a.speed = 1.0
		a.accumulated = 0
	}
}

// Enabled reports whether acceleration is active.
func (a *ScrollAcceleration) Enabled() bool {
	return a.enabled
}

// Tick records a scroll event and returns the multiplier.
// Short intervals between ticks increase speed up to 5x.
// Intervals longer than 500ms reset to 1x.
func (a *ScrollAcceleration) Tick() float64 {
	if !a.enabled {
		return 1.0
	}
	now := time.Now()
	elapsed := now.Sub(a.lastTick)
	a.lastTick = now
	if elapsed > 500*time.Millisecond {
		a.speed = 1.0
		a.accumulated = 0
		return 1.0
	}
	a.accumulated++
	// Exponential speed: 1, 2, 3, 4, 5 max
	a.speed = 1.0 + float64(a.accumulated)*0.5
	if a.speed > 5.0 {
		a.speed = 5.0
	}
	return a.speed
}

// Reset returns speed to baseline.
func (a *ScrollAcceleration) Reset() {
	a.speed = 1.0
	a.accumulated = 0
	a.lastTick = time.Now()
}

// Speed returns the current multiplier.
func (a *ScrollAcceleration) Speed() float64 {
	return a.speed
}
