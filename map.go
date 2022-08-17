package mamo

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type mamoMapEventKind int

const (
	mmekUnknown mamoMapEventKind = iota
	mmekIdle
	mmekQuit
	mmekTick
)

type mamoMapEvent struct {
	kind    mamoMapEventKind
	payload any
}

type mamoMapIdleEventPayload[K comparable, V any] struct {
	key     K
	entry   *mamoEntry[V]
	prevVer uint32
}

type ReleaseFunc func()

var releaseNoop = func() {}

type mamoEntryFlag = uint32

const (
	mefUnknown mamoEntryFlag = iota
	mefInitializing
	mefReady
	mefDeleting
	mefDeleted
)

type mamoEntry[V any] struct {
	cnt   int32
	ver   uint32
	flag  mamoEntryFlag
	value *V
}

func (e *mamoEntry[V]) untouchedSince(ver uint32) bool {
	return atomic.LoadInt32(&e.cnt) == 0 && atomic.LoadUint32(&e.ver) == ver
}

func (e *mamoEntry[V]) acquire() bool {
	atomic.AddInt32(&e.cnt, +1)

	var flag = atomic.LoadUint32(&e.flag)
	switch flag {
	case mefInitializing:
		for atomic.LoadUint32(&e.flag) == mefInitializing {
			runtime.Gosched()
		}
	case mefReady:
		break
	case mefDeleting:
	DELETING_LOOP:
		for {
			switch atomic.LoadUint32(&e.flag) {
			case mefReady:
				break DELETING_LOOP
			case mefDeleting:
				runtime.Gosched()
			case mefDeleted:
				return false
			}
		}
	case mefDeleted:
		return false
	}

	atomic.AddUint32(&e.ver, 1)
	return true
}

type Entry struct {
	acquireFunc func() bool
	releaseFunc func()
}

func newEntry[K comparable, V any](m *MamoMap[K, V], key K, e *mamoEntry[V]) Entry {
	return Entry{
		acquireFunc: e.acquire,
		releaseFunc: func() { m.releaseEntry(key, e) },
	}
}

func (e Entry) Acquire() bool { return e.acquireFunc() }
func (e Entry) Release()      { e.releaseFunc() }

type MamoMap[K comparable, V any] struct {
	once sync.Once
	q    chan mamoMapEvent
	m    sync.Map
	ttl  time.Duration

	diedCh chan struct{}

	isFinalizable bool
	isFastDelete  bool
}

func NewMap[K comparable, V any](ttl time.Duration) *MamoMap[K, V] {
	return &MamoMap[K, V]{
		q:             make(chan mamoMapEvent),
		ttl:           ttl,
		diedCh:        make(chan struct{}),
		isFinalizable: isFinalizable[V](),
		isFastDelete:  ttl == 0,
	}
}

func (m *MamoMap[K, V]) releaseEntry(key K, entry *mamoEntry[V]) {
	ver := atomic.AddUint32(&entry.ver, 1)
	cnt := atomic.AddInt32(&entry.cnt, -1)
	if cnt < 0 {
		panic(fmt.Sprintf("bad refcnt %d", cnt))
	} else if cnt == 0 {
		if m.isFastDelete {
			m.tryDelete(key, entry, atomic.LoadUint32(&entry.ver))
		} else {
			select {
			case <-m.diedCh:
				m.tryDelete(key, entry, atomic.LoadUint32(&entry.ver))
			case m.q <- mamoMapEvent{mmekIdle, mamoMapIdleEventPayload[K, V]{key, entry, ver}}:
			}
		}
	}
}

func (m *MamoMap[K, V]) tryDelete(key K, entry *mamoEntry[V], ver uint32) {
	if entry.untouchedSince(ver) &&
		atomic.CompareAndSwapUint32(&entry.flag, mefReady, mefDeleting) {
		var deleted bool
		if entry.untouchedSince(ver) {
			if entry.value != nil {
				if m.isFinalizable {
					var x any = (*entry.value)
					x.(Finalizable).MamoFinalize()
				}
				entry.value = nil
			}
			m.m.Delete(key)
			deleted = true

		}
		if deleted {
			atomic.StoreUint32(&entry.flag, mefDeleted)
		} else {
			atomic.StoreUint32(&entry.flag, mefReady)
		}
	}
}

func (m *MamoMap[K, V]) loop() {
	var (
		event    mamoMapEvent
		toCheck  []mamoMapIdleEventPayload[K, V]
		toDelete []mamoMapIdleEventPayload[K, V]
		timer    = newTimer(m.ttl)
	)

	defer timer.stop()
LOOP:
	for {
		select {
		case <-timer.c():
			event = mamoMapEvent{mmekTick, nil}
		case event = <-m.q:
		}
		switch event.kind {
		case mmekIdle:
			entry := event.payload.(mamoMapIdleEventPayload[K, V])
			toCheck = append(toCheck, entry)
			timer.start()
		case mmekQuit:
			close(m.diedCh)
			for _, payload := range toDelete {
				m.tryDelete(payload.key, payload.entry, payload.prevVer)
			}
			for _, payload := range toCheck {
				m.tryDelete(payload.key, payload.entry, payload.prevVer)
			}
			for {
				select {
				case event = <-m.q:
					if event.kind == mmekIdle {
						payload := event.payload.(mamoMapIdleEventPayload[K, V])
						m.tryDelete(payload.key, payload.entry, payload.prevVer)
					}
				default:
					return
				}
			}
		case mmekTick:
			if len(toDelete) == 0 && len(toCheck) == 0 {
				timer.stop()
				continue LOOP
			}
			for _, payload := range toDelete {
				m.tryDelete(payload.key, payload.entry, payload.prevVer)
			}
			toDelete = toDelete[:0]
			for _, payload := range toCheck {
				entry := payload.entry
				if entry.untouchedSince(payload.prevVer) {
					toDelete = append(toDelete, payload)
				}
			}
			toCheck = toCheck[:0]
			timer.stop()
			timer.start()
		}
	}
}

func (m *MamoMap[K, V]) ensureAlive() {
	m.Loop()

	select {
	case <-m.diedCh:
		panic("operating a died MamoMap")
	default:
	}

}

func (m *MamoMap[K, V]) Quit() {
	if m.isFastDelete {
		m.once.Do(func() { close(m.diedCh) })
		return
	}
	m.Loop()
	select {
	case <-m.diedCh:
	case m.q <- mamoMapEvent{mmekQuit, nil}:
	}
}

func (m *MamoMap[K, V]) Loop() {
	if m.isFastDelete {
		return
	}
	m.once.Do(func() { go m.loop() })
}

func (m *MamoMap[K, V]) AcquireOrStore(key K, createValue func(Entry) V) (ret V, created bool, release ReleaseFunc) {
	m.ensureAlive()
BEGIN:
	entry := &mamoEntry[V]{cnt: 1, flag: mefInitializing}
	actual, loaded := m.m.LoadOrStore(key, entry)
	if loaded {
		entry = actual.(*mamoEntry[V])
		atomic.AddInt32(&entry.cnt, +1)
		var flag = atomic.LoadUint32(&entry.flag)
		switch flag {
		case mefInitializing:
			for atomic.LoadUint32(&entry.flag) == mefInitializing {
				runtime.Gosched()
			}
		case mefReady:
			break
		case mefDeleting:
		DELETING_LOOP:
			for {
				flag := atomic.LoadUint32(&entry.flag)
				switch flag {
				case mefReady:
					break DELETING_LOOP
				case mefDeleting:
					runtime.Gosched()
				case mefDeleted:
					goto BEGIN
				}
			}
		case mefDeleted:
			goto BEGIN
		}
		created = false
	} else {
		value := createValue(newEntry(m, key, entry))
		entry.value = &value
		atomic.StoreUint32(&entry.flag, mefReady)
		created = true
	}
	atomic.AddUint32(&entry.ver, 1)
	return *entry.value, created, func() { m.releaseEntry(key, entry) }
}

func (m *MamoMap[K, V]) Acquire(key K) (ret V, acquired bool, release ReleaseFunc) {
	m.ensureAlive()

	acquired = false
	release = releaseNoop
	actual, loaded := m.m.Load(key)
	if !loaded {
		return
	}
	entry := actual.(*mamoEntry[V])

	if !entry.acquire() {
		return
	} else {
		return *entry.value, true, func() { m.releaseEntry(key, entry) }
	}
}

func (m *MamoMap[K, V]) Release(key K) {
	actual, loaded := m.m.Load(key)
	if !loaded {
		return
	}
	m.releaseEntry(key, actual.(*mamoEntry[V]))
}
