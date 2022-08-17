package mamo_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hsfzxjy/mamo"
)

func TestMapEx(t *testing.T) {
	m := mamo.NewMapEx[int, int](10 * time.Millisecond)
	newer := func() (int, error) {
		time.Sleep(500 * time.Millisecond)
		return 1, nil
	}
	wg := &sync.WaitGroup{}
	var ncreats int32 = 0
	var results int32 = 0
	var res *mamo.MapExResult[int]
	wg.Add(3)
	for i := 1; i <= 3; i++ {
		go func(j int) {
			var created bool
			var release mamo.ReleaseFunc
			res, created, release = m.AcquireOrStore(1, newer)
			if created {
				atomic.AddInt32(&ncreats, 1)
			}
			atomic.AddInt32(&results, int32(res.Value()))
			wg.Done()
			release()
		}(i)
	}
	wg.Wait()
	if ncreats != 1 {
		t.Fatalf("ncreats %d != 1", ncreats)
	}
	if results != 3 {
		t.Fatalf("results %d != 3", results)
	}

	res.Revoke()
	res.Revoke()

	wg.Add(3)
	var nerrs int32 = 0
	ncreats = 0
	newer = func() (int, error) {
		time.Sleep(500 * time.Millisecond)
		return 1, errors.New("oops")
	}
	for i := 1; i <= 3; i++ {
		go func(j int) {
			res, created, release := m.AcquireOrStore(1, newer)
			if created {
				atomic.AddInt32(&ncreats, 1)
			}
			if res.IsError() {
				atomic.AddInt32(&nerrs, 1)
			}
			wg.Done()
			release()
		}(i)
	}
	wg.Wait()
	if ncreats != 0 {
		t.Fatalf("ncreats %d != 0", ncreats)
	}
	if nerrs != 3 {
		t.Fatalf("nerrs %d != 3", nerrs)
	}

	time.Sleep(30 * time.Millisecond)
	if _, acquired, _ := m.Acquire(1); acquired {
		t.Fatal("key 1 should not be acquired")
	}
}
