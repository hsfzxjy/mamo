package mamo_test

import (
	"testing"
	"time"

	"github.com/hsfzxjy/mamo"
)

func TestMamo(t *testing.T) {
	var flag int
	m := mamo.New(time.Millisecond*100, func() bool { flag = 1; return true })
	m.Loop()
	m.Acquire()
	m.Release()
	time.Sleep(150 * time.Millisecond)
	if flag != 1 {
		t.Fatalf("flag %d != 1", flag)
	}
}

func TestMamo2(t *testing.T) {
	var flag int
	m := mamo.New(time.Millisecond*100, func() bool { flag = 1; return true })
	m.Loop()
	m.Acquire()
	time.Sleep(time.Millisecond * 800)
	if flag != 0 {
		t.Fatalf("flag %d != 0", flag)
	}
	m.Release()
	time.Sleep(150 * time.Millisecond)
	if flag != 1 {
		t.Fatalf("flag %d != 1", flag)
	}
}
