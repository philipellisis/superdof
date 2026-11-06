package dof

import (
	"strings"
	"sync"
	"testing"
	"time"

	"superdof/internal/pinone"
)

func TestOutputTraceNamesTheSource(t *testing.T) {
	o := &Output{Name: "PinOne Controller 01.11", Number: 11}
	var mu sync.Mutex
	var msgs []string
	o.SetTrace(func(m string) { mu.Lock(); msgs = append(msgs, m); mu.Unlock() })
	o.SetExternal("dev:GR:0", 255)
	o.SetToy(80)
	o.SetExternal("dev:GR:0", 0)
	o.SetToy(0)
	mu.Lock()
	defer mu.Unlock()
	if len(msgs) != 3 { // 0->255, (toy 80 hidden behind 255: no change), 255->80, 80->0
		t.Fatalf("messages: %q", msgs)
	}
	if !strings.Contains(msgs[0], "DOFLinx dev:GR:0") || !strings.Contains(msgs[0], "0 -> 255") {
		t.Errorf("first message: %s", msgs[0])
	}
	if !strings.Contains(msgs[1], "255 -> 80") || !strings.Contains(msgs[2], "80 -> 0") {
		t.Errorf("messages: %q", msgs)
	}
}

func TestClearExternalDropsEverySource(t *testing.T) {
	o := &Output{Name: "x", Number: 1}
	o.SetExternal("a", 200)
	o.SetExternal("b", 100)
	o.clearExternal()
	if o.Value() != 0 {
		t.Errorf("value %d after clearExternal", o.Value())
	}
}

func TestPinOneRefreshResendsTheFullState(t *testing.T) {
	mem := &memTransport{}
	ctl := NewPinOneController(1, "mem", 1, nil)
	ctl.Opener = func(string) (pinone.Transport, error) { return mem, nil }
	ctl.SetRefresh(80 * time.Millisecond)
	var mu sync.Mutex
	var traced []string
	ctl.SetPacketTrace(map[int]bool{11: true}, func(m string) { mu.Lock(); traced = append(traced, m); mu.Unlock() })
	ctl.Start()
	defer ctl.Stop()
	ctl.Notify()
	waitFor(t, "startup frame", func() bool { mem.mu.Lock(); defer mem.mu.Unlock(); return len(mem.packets) >= 9 })
	mem.mu.Lock()
	n0 := len(mem.packets)
	mem.mu.Unlock()
	// nothing changes, yet the state is written again
	waitFor(t, "refresh", func() bool { mem.mu.Lock(); defer mem.mu.Unlock(); return len(mem.packets) >= n0+9 })
	mu.Lock()
	defer mu.Unlock()
	if len(traced) == 0 || !strings.Contains(traced[0], "outputs 8-14") {
		t.Errorf("packet trace for output 11: %q", traced)
	}
}
