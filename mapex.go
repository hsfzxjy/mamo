package mamo

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type MapExResult[V any] struct {
	call *call[V]
	item *itemEx[V]
}

func (r *MapExResult[V]) Value() V {
	if r.call.err != nil {
		panic("invalid attempt to get value from errored MapExResult")
	}
	return r.call.value
}

func (r *MapExResult[V]) IsError() bool { return r.call.err != nil }
func (r *MapExResult[V]) Err() error    { return r.call.err }
func (r *MapExResult[V]) Revoke() {
	if r.item != nil && r.call.err == nil {
		r.item.revoke(r.call.ver)
	}
}

func (r *MapExResult[V]) IsRevoked() bool {
	return r.item == nil || r.call.ver != atomic.LoadUint32(&r.item.ver)
}

type itemExState = int32

const (
	iesUnknown itemExState = iota
	iesUninitialized
	iesInitializing
	iesCallReady
	iesReady
	iesRevoking
)

type call[V any] struct {
	wg    *sync.WaitGroup
	value V
	ver   uint32
	err   error
}

type itemEx[V any] struct {
	state itemExState
	ver   uint32
	call  *call[V]
}

func newItemEx[V any]() *itemEx[V] {
	return &itemEx[V]{state: iesUninitialized}
}

func (item *itemEx[V]) revoke(ver uint32) {
	if atomic.CompareAndSwapInt32(&item.state, iesReady, iesRevoking) {
		if ver != atomic.LoadUint32(&item.ver) {
			atomic.StoreInt32(&item.state, iesReady)
			return
		}
		atomic.AddUint32(&item.ver, 1)
		item.call = nil
		atomic.StoreInt32(&item.state, iesUninitialized)
	}
}

func (i *itemEx[V]) do(createValue func() (V, error)) *call[V] {
	if atomic.CompareAndSwapInt32(&i.state, iesUninitialized, iesInitializing) {
		c := &call[V]{
			wg:  &sync.WaitGroup{},
			ver: i.ver,
		}
		c.wg.Add(1)
		i.call = c
		atomic.StoreInt32(&i.state, iesCallReady)

		var finalState = iesUnknown
		var err error
		var v V

		defer func() {
			if p := recover(); p != nil {
				err = newPanicError(p)
			}
			if err == nil {
				finalState = iesReady
				c.value = v
			} else {
				finalState = iesUninitialized
				c.err = err
			}
			atomic.StoreInt32(&i.state, finalState)
			c.wg.Done()
		}()

		v, err = createValue()
		return c
	}
	return nil
}

type MamoMapEx[K comparable, V any] struct {
	m *MamoMap[K, *itemEx[V]]
}

func NewMapEx[K comparable, V any](ttl time.Duration) *MamoMapEx[K, V] {
	m := new(MamoMapEx[K, V])
	m.m = NewMap[K, *itemEx[V]](ttl)
	return m
}

func (m *MamoMapEx[K, V]) AcquireOrStore(key K, createValue func() (V, error)) (res *MapExResult[V], created bool, release ReleaseFunc) {
	var item *itemEx[V]
	item, created, release = m.m.AcquireOrStore(key, newItemEx[V])
	var c *call[V]

LOAD_STATE:
	c = nil
	switch atomic.LoadInt32(&item.state) {
	case iesUninitialized:
		c = item.do(createValue)
		if c != nil {
			created = true
		}
	case iesInitializing:
	case iesCallReady, iesReady:
		c = item.call
	case iesRevoking:
	}

	if c == nil {
		runtime.Gosched()
		goto LOAD_STATE
	}

	c.wg.Wait()

	if c.ver != atomic.LoadUint32(&item.ver) {
		runtime.Gosched()
		goto LOAD_STATE
	}

	if c.err != nil {
		release()
		return &MapExResult[V]{c, nil}, false, releaseNoop
	} else {
		return &MapExResult[V]{c, item}, created, release
	}
}

func (m *MamoMapEx[K, V]) Acquire(key K) (res *MapExResult[V], acquired bool, release ReleaseFunc) {
	var item *itemEx[V]
	var c *call[V]
	item, acquired, release = m.m.Acquire(key)

	if !acquired {
		release()
		return nil, false, releaseNoop
	}

LOAD_STATE:
	c = nil
	switch atomic.LoadInt32(&item.state) {
	case iesUninitialized:
		release()
		return nil, false, releaseNoop
	case iesInitializing:
	case iesCallReady, iesReady:
		c = item.call
	case iesRevoking:
	}
	if c == nil {
		runtime.Gosched()
		goto LOAD_STATE
	}
	if c.ver != atomic.LoadUint32(&item.ver) {
		runtime.Gosched()
		goto LOAD_STATE
	}
	c.wg.Wait()
	if c.err != nil {
		release()
		return nil, false, releaseNoop
	} else {
		return &MapExResult[V]{c, item}, true, release
	}
}

func (m *MamoMapEx[K, V]) Release(key K) { m.m.Release(key) }
func (m *MamoMapEx[K, V]) Loop()         { m.m.Loop() }
func (m *MamoMapEx[K, V]) Quit()         { m.m.Quit() }
