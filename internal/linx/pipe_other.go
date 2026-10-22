//go:build !windows

package linx

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
)

// ListenPipe serves a unix domain socket named <tmp>/<name>.sock on
// non-Windows systems (the Windows only trigger DLL is not used there; this
// carries the command and bridge protocols).
func ListenPipe(name string, onConn func(c io.ReadWriteCloser)) (stop func(), err error) {
	path := filepath.Join(os.TempDir(), name+".sock")
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	var stopped atomic.Bool
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				if stopped.Load() {
					return
				}
				continue
			}
			go func() {
				defer c.Close()
				onConn(c)
			}()
		}
	}()
	return func() { stopped.Store(true); l.Close(); os.Remove(path) }, nil
}
