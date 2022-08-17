package mamo

import (
	"sync"
	"time"
)

type mamoEvent int

const (
	meUnknown mamoEvent = iota
	meAcquire
	meRelease
	meTick
	meQuit
)

type Mamo struct {
	eventQ chan mamoEvent
	diedCh chan struct{}

	loopOnce sync.Once

	notifier func() bool
	ttl      time.Duration
}

func New(ttl time.Duration, notifier func() bool) *Mamo {
	m := &Mamo{
		eventQ:   make(chan mamoEvent),
		diedCh:   make(chan struct{}),
		notifier: notifier,
		ttl:      ttl,
	}
	return m
}

func (m *Mamo) Acquire() bool { return m.send(meAcquire) }
func (m *Mamo) Release() bool { return m.send(meRelease) }
func (m *Mamo) Quit()         { m.send(meQuit) }
func (m *Mamo) Loop()         { m.loopOnce.Do(func() { go m.loop() }) }

func (m *Mamo) send(e mamoEvent) bool {
	m.Loop()
	select {
	case <-m.diedCh:
		return false
	case m.eventQ <- e:
		return true
	}
}

func (m *Mamo) loop() {
	var (
		counter int32 = 1

		ver     int32 = 0
		prevVer int32 = 0

		timer = newTimer(m.ttl)

		event mamoEvent = meRelease
	)

	defer func() {
		timer.stop()
		m.notifier = nil
		close(m.diedCh)
	}()

LOOP:
	for {
		switch event {
		case meAcquire:
			counter++
			ver++
			timer.stop()
		case meRelease:
			counter--
			ver++

			if counter == 0 {
				prevVer = ver
				timer.stop()
				timer.start()
			}
		case meQuit:
			return
		case meTick:
			if ver != prevVer {
				continue LOOP
			}
			if !m.notifier() {
				prevVer = ver
				timer.stop()
			} else {
				return
			}
		}
		select {
		case event = <-m.eventQ:
		case <-timer.c():
			event = meTick
		}
	}
}
