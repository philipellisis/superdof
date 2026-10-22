package linx

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

type rwc struct {
	io.Reader
	out bytes.Buffer
}

func (r *rwc) Write(p []byte) (int, error) { return r.out.Write(p) }
func (r *rwc) Close() error                { return nil }

type bridgeDOF struct {
	fakeDOF
	sent []string
}

func (b *bridgeDOF) SendNamed(d string, v int) { b.sent = append(b.sent, fmt.Sprintf("%s=%d", d, v)) }

func TestBridgeProtocol(t *testing.T) {
	d := &bridgeDOF{}
	br := &Bridge{DOF: d, Log: nopLog{}, Version: "t", RomFor: func(table, rom string) string {
		if rom != "" {
			return rom
		}
		return "mappedrom"
	}}
	in := strings.NewReader("HELLO\r\nINIT|C:\\tables\\Foo.vpx|\r\nD|E|101|1\r\nD|E|101|0\r\nN|Name|1\r\nFINISH\r\n")
	c := &rwc{Reader: in}
	br.Serve(c)
	out := c.out.String()
	if !strings.Contains(out, "OK SuperDOF t") || !strings.Contains(out, "OK\r\n") {
		t.Fatalf("unexpected replies %q", out)
	}
	if len(d.roms) != 1 || d.roms[0] != "mappedrom" {
		t.Fatalf("roms %v", d.roms)
	}
	if len(d.sent) != 3 || d.sent[0] != "E101=1" || d.sent[1] != "E101=0" || d.sent[2] != "$Name=1" {
		t.Fatalf("sent %v", d.sent)
	}
}
