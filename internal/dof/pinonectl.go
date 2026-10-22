package dof

import (
	"fmt"
	"sync"
	"time"

	"superdof/internal/pinone"
)

// PinOneOutputs is the number of outputs on a PinOne board (31 on board plus
// 32 on expansion boards).
const PinOneOutputs = 63

// PinOneController drives a PinOne board over USB HID (current firmware) or
// CDC serial (legacy firmware). It is the Go counterpart of
// DirectOutput.Cab.Out.PinOne.PinOne.
type PinOneController struct {
	name     string
	number   int
	port     string
	interval time.Duration
	log      Logger
	outputs  []*Output

	mu      sync.Mutex
	state   int // 0 startup, 1 value changed, 2 running
	kick    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	running bool

	// Opener can be replaced in tests.
	Opener func(port string) (pinone.Transport, error)
}

// NewPinOneController creates unit number n (1..8). port is "", "auto",
// "hid" or a serial port name.
func NewPinOneController(n int, port string, intervalMs int, log Logger) *PinOneController {
	if log == nil {
		log = nopLogger{}
	}
	if intervalMs < 1 {
		intervalMs = 1
	}
	c := &PinOneController{
		name:     fmt.Sprintf("PinOne Controller %02d", n),
		number:   n,
		port:     port,
		interval: time.Duration(intervalMs) * time.Millisecond,
		log:      log,
		kick:     make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		Opener:   pinone.Open,
	}
	for i := 1; i <= PinOneOutputs; i++ {
		c.outputs = append(c.outputs, &Output{Name: fmt.Sprintf("%s.%d", c.name, i), Number: i})
	}
	return c
}

// Name implements Controller.
func (c *PinOneController) Name() string { return c.name }

// Number is the unit number; its LedWizEquivalent number is Number+10.
func (c *PinOneController) Number() int { return c.number }

// Outputs implements Controller.
func (c *PinOneController) Outputs() []*Output { return c.outputs }

// Notify wakes the updater goroutine.
func (c *PinOneController) Notify() {
	c.mu.Lock()
	if c.state == 0 {
		c.state = 1
	}
	c.mu.Unlock()
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Start launches the updater goroutine.
func (c *PinOneController) Start() error {
	if c.running {
		return nil
	}
	c.running = true
	go c.run()
	return nil
}

// Stop turns all outputs off and closes the device.
func (c *PinOneController) Stop() {
	if !c.running {
		return
	}
	c.running = false
	close(c.stop)
	<-c.done
}

func (c *PinOneController) snapshot() [PinOneOutputs]byte {
	var v [PinOneOutputs]byte
	for i, o := range c.outputs {
		v[i] = o.Value()
	}
	return v
}

func (c *PinOneController) run() {
	defer close(c.done)
	var tr pinone.Transport
	var old [PinOneOutputs]byte
	for i := range old {
		old[i] = 255
	}
	lastWrite := time.Now()
	lastWarn := time.Time{}

	send := func(vals [PinOneOutputs]byte) error {
		pfx := byte(200)
		for i := 0; i < PinOneOutputs; i, pfx = i+7, pfx+1 {
			lim := i + 7
			if lim > PinOneOutputs {
				lim = PinOneOutputs
			}
			changed := false
			for j := i; j < lim; j++ {
				if vals[j] != old[j] {
					changed = true
					break
				}
			}
			if !changed {
				continue
			}
			if d := time.Since(lastWrite); d < c.interval {
				time.Sleep(c.interval - d)
			}
			buf := make([]byte, 9)
			buf[0] = 0
			buf[1] = pfx
			copy(buf[2:], vals[i:lim])
			if err := tr.Write(buf); err != nil {
				return err
			}
			lastWrite = time.Now()
			copy(old[i:lim], vals[i:lim])
		}
		return nil
	}

	finish := func() {
		if tr != nil {
			c.mu.Lock()
			st := c.state
			c.mu.Unlock()
			if st != 0 {
				var zero [PinOneOutputs]byte
				_ = send(zero)
			}
			tr.Close()
			tr = nil
		}
	}

	for {
		if tr == nil {
			t, err := c.Opener(c.port)
			if err != nil {
				if time.Since(lastWarn) > 30*time.Second {
					c.log.Warnf("%s: %v (will keep retrying)", c.name, err)
					lastWarn = time.Now()
				}
				select {
				case <-c.stop:
					return
				case <-time.After(time.Second):
				}
				continue
			}
			tr = t
			c.log.Infof("%s connected via %s", c.name, tr.Describe())
			for i := range old {
				old[i] = 255
			}
			c.mu.Lock()
			if c.state == 2 {
				c.state = 1
			}
			c.mu.Unlock()
		}

		c.mu.Lock()
		st := c.state
		c.mu.Unlock()
		var err error
		if st == 1 {
			var zero [PinOneOutputs]byte
			err = send(zero)
			c.mu.Lock()
			c.state = 2
			c.mu.Unlock()
			st = 2
		}
		if err == nil && st == 2 {
			err = send(c.snapshot())
		}
		if err != nil {
			c.log.Warnf("%s: update failed: %v; reconnecting", c.name, err)
			tr.Close()
			tr = nil
			c.mu.Lock()
			if c.state == 2 {
				c.state = 1
			}
			c.mu.Unlock()
			select {
			case <-c.stop:
				return
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}

		select {
		case <-c.stop:
			finish()
			return
		case <-c.kick:
		case <-time.After(50 * time.Millisecond):
		}
	}
}
