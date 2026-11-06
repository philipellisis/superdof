package linx

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func newAllOffRunner() (*Runner, *fakeSink, *fakeDOF) {
	cfg := NewConfig()
	cfg.addLine("LINK_GR=1011,750,10000,255")
	cfg.addLine("LINK_SH=1012,1000,5000,255")
	cfg.addLine("LINK_KN=1013,120,500,255")
	fs, fd := &fakeSink{}, &fakeDOF{}
	return NewRunner(cfg, fd, fs, nil, nopLog{}), fs, fd
}

// Release used a different source key than the one the outputs were set with,
// so the gear / shaker / knocker outputs stayed on for good.
func TestAllOffReleasesDevicesAndOutputNow(t *testing.T) {
	r, fs, _ := newAllOffRunner()
	r.Execute(Action{Name: "FF_DEV", Args: "DV_GR,10000"})
	r.Execute(Action{Name: "FF_DEV", Args: "DV_SH,5000"})
	r.OutputNow(1013, 60000)
	waitUntil(t, "on", func() bool { return fs.get(1, 11) > 0 && fs.get(1, 12) > 0 && fs.get(1, 13) > 0 })
	r.AllOff()
	for p := 11; p <= 13; p++ {
		if v := fs.get(1, p); v != 0 {
			t.Errorf("output %d still %d after AllOff", p, v)
		}
	}
}

func TestBeginStartsFromSilence(t *testing.T) {
	r, fs, _ := newAllOffRunner()
	r.Execute(Action{Name: "FF_DEV", Args: "DV_KN,500"})
	waitUntil(t, "knocker", func() bool { return fs.get(1, 13) > 0 })
	// no game was active, yet a new game must clear what is still on
	r.Begin("NewTable", NewCommandSet(), nil)
	if v := fs.get(1, 13); v != 0 {
		t.Errorf("knocker still %d after Begin", v)
	}
}

func TestBridgeClientVanishingSwitchesOutputsOff(t *testing.T) {
	r, fs, fd := newAllOffRunner()
	b := &Bridge{DOF: fd, Runner: r, Log: nopLog{}, Version: "t"}
	srv, cli := net.Pipe()
	done := make(chan struct{})
	go func() { b.Serve(srv); close(done) }()
	w := bufio.NewWriter(cli)
	rd := bufio.NewReader(cli)
	w.WriteString("INIT|table.vpx|afm\r\n")
	w.Flush()
	if l, _ := rd.ReadString('\n'); !strings.HasPrefix(l, "OK") {
		t.Fatalf("INIT reply %q", l)
	}
	r.Execute(Action{Name: "FF_DEV", Args: "DV_GR,10000"})
	waitUntil(t, "gear on", func() bool { return fs.get(1, 11) > 0 })
	cli.Close() // the front end was killed: no FINISH
	<-done
	if v := fs.get(1, 11); v != 0 {
		t.Errorf("gear still %d after the client vanished", v)
	}
}

func TestBridgeINITClearsPreviousOutputs(t *testing.T) {
	r, fs, fd := newAllOffRunner()
	b := &Bridge{DOF: fd, Runner: r, Log: nopLog{}, Version: "t"}
	r.Execute(Action{Name: "FF_DEV", Args: "DV_SH,5000"})
	waitUntil(t, "shaker on", func() bool { return fs.get(1, 12) > 0 })
	srv, cli := net.Pipe()
	go b.Serve(srv)
	defer cli.Close()
	w := bufio.NewWriter(cli)
	w.WriteString("INIT|t.vpx|rom\r\n")
	w.Flush()
	bufio.NewReader(cli).ReadString('\n')
	if v := fs.get(1, 12); v != 0 {
		t.Errorf("shaker still %d after a new INIT", v)
	}
}
