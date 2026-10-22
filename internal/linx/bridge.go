package linx

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Bridge serves the SuperDOF line protocol used by the B2S Server plugin
// shim (VPX / Visual Pinball X, PinMAME, ...). Replacement for loading
// DirectOutput.dll inside the B2S server process.
//
// Lines (UTF-8, '|' separated, one per message):
//
//	HELLO                       -> OK SuperDOF <version>
//	INIT|<tableFile>|<rom>      -> OK / ERR   (load the DOF config for the table)
//	D|<type char>|<number>|<value>           (table element update, no reply)
//	N|<name>|<value>                         (named element update, no reply)
//	FINISH                                   (table closed, outputs off)
type Bridge struct {
	DOF     DOFSender
	Runner  *Runner
	RomFor  func(tableFile, rom string) string
	Log     Logger
	Version string
}

// DOFSender is the DOF engine surface used by the bridge.
type DOFSender interface {
	DOF
	SendNamed(descriptor string, value int)
}

// Serve handles one client connection until it closes.
func (b *Bridge) Serve(c io.ReadWriteCloser) {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		switch strings.ToUpper(f[0]) {
		case "HELLO":
			fmt.Fprintf(c, "OK SuperDOF %s\r\n", b.Version)
		case "INIT":
			table, rom := "", ""
			if len(f) > 1 {
				table = f[1]
			}
			if len(f) > 2 {
				rom = f[2]
			}
			if b.RomFor != nil {
				rom = b.RomFor(table, rom)
			}
			if rom == "" {
				b.Log.Warnf("bridge: no ROM name for table %q (add it to tablemappings.xml)", table)
				fmt.Fprint(c, "ERR no rom\r\n")
				continue
			}
			if b.Runner != nil {
				b.Runner.End()
			}
			b.Log.Infof("bridge: table %q rom %q", table, rom)
			done := make(chan bool, 1)
			b.DOF.LoadRom(rom, func(ok bool) { done <- ok })
			if <-done {
				fmt.Fprint(c, "OK\r\n")
			} else {
				fmt.Fprint(c, "ERR no config\r\n")
			}
		case "D":
			if len(f) >= 4 && len(f[1]) == 1 {
				n, _ := strconv.Atoi(f[2])
				v, _ := strconv.Atoi(f[3])
				b.DOF.SendNamed(fmt.Sprintf("%c%d", f[1][0], n), v)
			}
		case "N":
			if len(f) >= 3 {
				v, _ := strconv.Atoi(f[2])
				b.DOF.SendNamed("$"+f[1], v)
			}
		case "FINISH":
			b.DOF.Clear()
			fmt.Fprint(c, "OK\r\n")
		}
	}
}
