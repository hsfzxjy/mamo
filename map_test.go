package mamo_test

import (
	"mamo"
	"sync/atomic"
	"testing"
	"time"
)

func TestMap(t *testing.T) {
	var (
		loaded bool
		r      mamo.ReleaseFunc = func() {}
	)
	m := mamo.NewMap[int, int](10 * time.Millisecond)

	_, created, release := m.AcquireOrStore(1, func() int { return 1 })
	release()
	time.Sleep(40 * time.Millisecond)
	if _, loaded, _ = m.Acquire(1); loaded || !created {
		t.Fatal(1)
	}

	_, created, release = m.AcquireOrStore(1, func() int { return 1 })
	time.Sleep(20 * time.Millisecond)
	if _, loaded, r = m.Acquire(1); !loaded || !created {
		t.Fatal(2)
	}
	r()
	release()
	time.Sleep(50 * time.Millisecond)

	_, created, release = m.AcquireOrStore(1, func() int { return 1 })
	if !created {
		t.Fatal(3)
	}
	m.Quit()
	release()
}

type MyFinalizable struct {
	counter *int32
}

func (x MyFinalizable) MamoFinalize() error { atomic.AddInt32(x.counter, 1); return nil }

func TestMapWithFinalizable(t *testing.T) {
	var loaded bool
	var r mamo.ReleaseFunc = func() {}
	var counter int32
	var newCounter int32
	m := mamo.NewMap[int, MyFinalizable](10 * time.Millisecond)

	newer := func() MyFinalizable {
		atomic.AddInt32(&newCounter, 1)
		return MyFinalizable{&counter}
	}
	_, created, release := m.AcquireOrStore(1, newer)
	release()
	time.Sleep(30 * time.Millisecond)
	if _, loaded, _ := m.Acquire(1); loaded || !created {
		t.Fatalf("1")
	}

	_, created, release = m.AcquireOrStore(1, newer)
	time.Sleep(20 * time.Millisecond)
	if _, loaded, r = m.Acquire(1); !loaded || !created {
		t.Fatalf("2")
	}
	r()
	release()
	time.Sleep(40 * time.Millisecond)

	_, created, release = m.AcquireOrStore(1, newer)
	if !created {
		t.Fatalf("3")
	}
	m.Quit()
	release()

	if newCounter != 3 {
		t.Fatalf("newCounter %d != 3", newCounter)
	}

	if counter != 3 {
		t.Fatalf("counter %d != 3", counter)
	}
}

func TestMapFastDelete(t *testing.T) {
	var loaded bool
	var r mamo.ReleaseFunc = func() {}
	var counter int32
	var newCounter int32
	m := mamo.NewMap[int, MyFinalizable](0)

	newer := func() MyFinalizable {
		atomic.AddInt32(&newCounter, 1)
		return MyFinalizable{&counter}
	}
	_, created, release := m.AcquireOrStore(1, newer)
	release()
	if _, loaded, _ := m.Acquire(1); loaded || !created {
		t.Fatalf("1")
	}

	_, created, release = m.AcquireOrStore(1, newer)
	if _, loaded, r = m.Acquire(1); !loaded || !created {
		t.Fatalf("2")
	}
	r()
	release()

	_, created, release = m.AcquireOrStore(1, newer)
	if !created {
		t.Fatalf("3")
	}
	m.Quit()
	release()

	if newCounter != 3 {
		t.Fatalf("newCounter %d != 3", newCounter)
	}

	if counter != 3 {
		t.Fatalf("counter %d != 3", counter)
	}
}
