package mamo

import "time"

type timer struct {
	inner    *time.Timer
	duration time.Duration
	dummyC   chan time.Time
}

func newTimer(duration time.Duration) *timer {
	return &timer{duration: duration, dummyC: make(chan time.Time)}
}

func (t *timer) c() <-chan time.Time {
	if t.inner == nil {
		return t.dummyC
	} else {
		return t.inner.C
	}
}

func (t *timer) start() {
	if t.inner == nil {
		t.inner = time.NewTimer(t.duration)
	}
}

func (t *timer) stop() {
	if t.inner != nil {
		t.inner.Stop()
		t.inner = nil
	}
}
