package navigation

import (
	"testing"
	"time"
)

func TestScrollAcceleration_Disabled(t *testing.T) {
	a := NewScrollAcceleration()
	speed := a.Tick()
	if speed != 1.0 {
		t.Errorf("want 1.0, got %f", speed)
	}
}

func TestScrollAcceleration_Enabled(t *testing.T) {
	a := NewScrollAcceleration()
	a.Enable(true)
	if !a.Enabled() {
		t.Error("should be enabled")
	}
}

func TestScrollAcceleration_SpeedsUp(t *testing.T) {
	a := NewScrollAcceleration()
	a.Enable(true)
	a.lastTick = time.Now().Add(-100 * time.Millisecond)
	speed := a.Tick()
	if speed <= 1.0 {
		t.Errorf("expected speed > 1.0, got %f", speed)
	}
}

func TestScrollAcceleration_Reset(t *testing.T) {
	a := NewScrollAcceleration()
	a.Enable(true)
	a.speed = 3.0
	a.accumulated = 4
	a.Reset()
	if a.speed != 1.0 {
		t.Errorf("want speed 1.0, got %f", a.speed)
	}
	if a.accumulated != 0 {
		t.Errorf("want accumulated 0, got %f", a.accumulated)
	}
}

func TestScrollAcceleration_LongInterval(t *testing.T) {
	a := NewScrollAcceleration()
	a.Enable(true)
	a.lastTick = time.Now().Add(-1 * time.Second)
	speed := a.Tick()
	if speed != 1.0 {
		t.Errorf("want 1.0 after long interval, got %f", speed)
	}
}

func TestScrollAcceleration_CapsAtFive(t *testing.T) {
	a := NewScrollAcceleration()
	a.Enable(true)
	for i := 0; i < 20; i++ {
		a.lastTick = time.Now().Add(-50 * time.Millisecond)
		speed := a.Tick()
		if speed > 5.0 {
			t.Errorf("speed capped at 5.0, got %f", speed)
		}
	}
}
