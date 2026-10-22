package dof

import (
	"fmt"
	"sync"
	"time"

	"superdof/internal/serial"
)

// ValueSetter is implemented by controllers that accept raw output values
// for a range of outputs (LED strips).
type ValueSetter interface {
	SetValues(first int, v []byte)
}

// StripConfig configures a Teensy / Wemos D1 mini addressable LED strip
// controller (DOF TeensyStripController / WemosD1MPStripController).
type StripConfig struct {
	Name          string
	ComPort       string
	Baud          int // default 9600 (Teensy); Wemos setups use 2000000
	TimeoutMs     int // default 200
	OpenWaitMs    int // default 50
	HandshakeMs   int // start wait, default 20
	HandshakeEnd  int // end wait, default 50
	DTR           bool
	LedsPerStrip  [10]int
	PerStripSize  bool // Wemos: send each strip's length ('Z')
	Compression   bool // Wemos: run length compression ('Q')
	TestOnConnect bool // Wemos: run the controller's test sequence once
}

// StripController implements the Teensy/Wemos serial LED protocol.
type StripController struct {
	cfg  StripConfig
	log  Logger
	mu   sync.Mutex
	vals []byte
	dirt bool

	kick chan struct{}
	stop chan struct{}
	done chan struct{}
	run  bool

	// Open can be replaced in tests.
	Open func(serial.Config) (serial.Port, error)

	port        serial.Port
	ledsChannel int
}

// NewStripController creates the controller.
func NewStripController(cfg StripConfig, log Logger) *StripController {
	if log == nil {
		log = nopLogger{}
	}
	if cfg.Baud == 0 {
		cfg.Baud = 9600
	}
	if cfg.TimeoutMs < 1 || cfg.TimeoutMs > 5000 {
		cfg.TimeoutMs = 200
	}
	if cfg.OpenWaitMs < 50 || cfg.OpenWaitMs > 5000 {
		cfg.OpenWaitMs = 50
	}
	if cfg.HandshakeMs < 20 || cfg.HandshakeMs > 500 {
		cfg.HandshakeMs = 20
	}
	if cfg.HandshakeEnd < 50 || cfg.HandshakeEnd > 500 {
		cfg.HandshakeEnd = 50
	}
	total := 0
	for _, n := range cfg.LedsPerStrip {
		total += n
	}
	return &StripController{cfg: cfg, log: log, vals: make([]byte, total*3), kick: make(chan struct{}, 1),
		stop: make(chan struct{}), done: make(chan struct{}), Open: serial.Open}
}

func (c *StripController) Name() string       { return c.cfg.Name }
func (c *StripController) Outputs() []*Output { return nil }
func (c *StripController) Notify()            {}

// NumberOfLeds is the total number of LEDs on all strips.
func (c *StripController) NumberOfLeds() int { return len(c.vals) / 3 }

// Snapshot returns a copy of the current channel values (R,G,B per LED as sent).
func (c *StripController) Snapshot() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.vals...)
}

// SetValues copies raw channel values (R,G,B per LED) starting at output first.
func (c *StripController) SetValues(first int, v []byte) {
	if first < 0 || first >= len(c.vals) {
		return
	}
	n := len(v)
	if first+n > len(c.vals) {
		n = len(c.vals) - first
	}
	c.mu.Lock()
	changed := false
	for i := 0; i < n; i++ {
		if c.vals[first+i] != v[i] {
			c.vals[first+i] = v[i]
			changed = true
		}
	}
	if changed {
		c.dirt = true
	}
	c.mu.Unlock()
	if changed {
		select {
		case c.kick <- struct{}{}:
		default:
		}
	}
}

func (c *StripController) Start() error {
	if c.run {
		return nil
	}
	if c.cfg.ComPort == "" {
		return fmt.Errorf("no ComPort configured for %s", c.cfg.Name)
	}
	c.run = true
	go c.loop()
	return nil
}

func (c *StripController) Stop() {
	if !c.run {
		return
	}
	c.run = false
	close(c.stop)
	<-c.done
}

func (c *StripController) loop() {
	defer close(c.done)
	lastWarn := time.Time{}
	for {
		if c.port == nil {
			if err := c.connect(); err != nil {
				if time.Since(lastWarn) > 30*time.Second {
					c.log.Warnf("%s: %v (will keep retrying)", c.cfg.Name, err)
					lastWarn = time.Now()
				}
				c.disconnect()
				select {
				case <-c.stop:
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			c.log.Infof("%s connected on %s (%d LEDs)", c.cfg.Name, c.cfg.ComPort, c.NumberOfLeds())
			c.mu.Lock()
			c.dirt = true
			c.mu.Unlock()
		}
		c.mu.Lock()
		send := c.dirt
		var snap []byte
		if send {
			snap = append([]byte(nil), c.vals...)
			c.dirt = false
		}
		c.mu.Unlock()
		if send {
			if err := c.update(snap); err != nil {
				c.log.Warnf("%s: %v; reconnecting", c.cfg.Name, err)
				c.disconnect()
				continue
			}
		}
		select {
		case <-c.stop:
			if c.port != nil {
				_ = c.update(make([]byte, len(c.vals)))
			}
			c.disconnect()
			return
		case <-c.kick:
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (c *StripController) disconnect() {
	if c.port != nil {
		c.port.Close()
		c.port = nil
	}
}

func (c *StripController) readWait(buf []byte) error {
	for i := range buf {
		one := make([]byte, 1)
		n, err := c.port.Read(one)
		if err != nil || n != 1 {
			return fmt.Errorf("reading byte %d of %d: %v", i+1, len(buf), err)
		}
		buf[i] = one[0]
	}
	return nil
}

func (c *StripController) expectAck(what string) error {
	b := make([]byte, 1)
	if err := c.readWait(b); err != nil {
		return fmt.Errorf("waiting for ACK after %s: %w", what, err)
	}
	if b[0] != 'A' {
		return fmt.Errorf("unexpected answer %q after %s (expected ACK)", b[0], what)
	}
	return nil
}

func (c *StripController) connect() error {
	p, err := c.Open(serial.Config{Name: c.cfg.ComPort, Baud: c.cfg.Baud, DTR: c.cfg.DTR, TimeoutMs: c.cfg.TimeoutMs})
	if err != nil {
		return err
	}
	c.port = p
	time.Sleep(time.Duration(c.cfg.OpenWaitMs) * time.Millisecond)
	p.Flush()
	ok := false
	for attempt := 0; attempt < 20 && !ok; attempt++ {
		p.Write([]byte{0})
		time.Sleep(time.Duration(c.cfg.HandshakeMs) * time.Millisecond)
		if p.Available() > 0 {
			b := make([]byte, 1)
			if n, _ := p.Read(b); n == 1 && (b[0] == 'A' || b[0] == 'N') {
				ok = true
				break
			}
		} else {
			p.Write(make([]byte, 3*1000))
			time.Sleep(time.Duration(c.cfg.HandshakeEnd) * time.Millisecond)
			p.Flush()
		}
	}
	if !ok {
		return fmt.Errorf("could not put the controller on %s into command mode", c.cfg.ComPort)
	}
	// 'M': max LEDs per channel
	p.Write([]byte{'M'})
	m := make([]byte, 3)
	if err := c.readWait(m); err != nil {
		return fmt.Errorf("max LEDs per channel: %w", err)
	}
	if m[2] != 'A' {
		return fmt.Errorf("no ACK after max LEDs per channel")
	}
	maxLeds := int(m[0])*256 + int(m[1])
	most := 0
	for _, n := range c.cfg.LedsPerStrip {
		if n > most {
			most = n
		}
	}
	if most > maxLeds {
		return fmt.Errorf("controller supports %d LEDs per channel but %d are configured", maxLeds, most)
	}
	c.ledsChannel = most
	p.Write([]byte{'L', byte(most >> 8), byte(most & 255)})
	if err := c.expectAck("setting LEDs per channel"); err != nil {
		return err
	}
	if c.cfg.PerStripSize {
		for i, n := range c.cfg.LedsPerStrip {
			if n > 0 {
				p.Write([]byte{'Z', byte(i), byte(len(c.cfg.LedsPerStrip) - 1), byte(n >> 8), byte(n & 255)})
				if err := c.expectAck(fmt.Sprintf("resizing strip %d", i)); err != nil {
					return err
				}
			}
		}
	}
	if c.cfg.TestOnConnect {
		p.Write([]byte{'T'})
		time.Sleep(2 * time.Second)
		if err := c.expectAck("test sequence"); err != nil {
			return err
		}
		c.cfg.TestOnConnect = false
	}
	p.Write([]byte{'C'})
	if err := c.expectAck("clearing the buffer"); err != nil {
		return err
	}
	p.Write([]byte{'O'})
	return c.expectAck("output")
}

func (c *StripController) sendStrip(data []byte, target int) error {
	nLeds := len(data) / 3
	if c.cfg.Compression {
		comp := compressRuns(data)
		if len(comp) < len(data) {
			nData := len(comp) / 4
			hdr := []byte{'Q', byte(target >> 8), byte(target & 255), byte(nData >> 8), byte(nData & 255), byte(nLeds >> 8), byte(nLeds & 255)}
			if _, err := c.port.Write(hdr); err != nil {
				return err
			}
			_, err := c.port.Write(comp)
			return err
		}
	}
	hdr := []byte{'R', byte(target >> 8), byte(target & 255), byte(nLeds >> 8), byte(nLeds & 255)}
	if _, err := c.port.Write(hdr); err != nil {
		return err
	}
	_, err := c.port.Write(data)
	return err
}

// compressRuns encodes RGB data as (count,r,g,b) records.
func compressRuns(d []byte) []byte {
	var out []byte
	for i := 0; i < len(d); {
		if len(d)-i == 3 {
			out = append(out, 1, d[i], d[i+1], d[i+2])
			break
		}
		r, g, b := d[i], d[i+1], d[i+2]
		cnt := 1
		i += 3
		for i+2 < len(d) && d[i] == r && d[i+1] == g && d[i+2] == b && cnt < 254 {
			cnt++
			i += 3
		}
		out = append(out, byte(cnt), r, g, b)
	}
	return out
}

func (c *StripController) update(all []byte) error {
	src := 0
	for i, n := range c.cfg.LedsPerStrip {
		if n <= 0 {
			continue
		}
		if err := c.sendStrip(all[src*3:(src+n)*3], i*c.ledsChannel); err != nil {
			return err
		}
		if err := c.expectAck(fmt.Sprintf("data of channel %d", i+1)); err != nil {
			return err
		}
		src += n
	}
	if _, err := c.port.Write([]byte{'O'}); err != nil {
		return err
	}
	return c.expectAck("output command")
}
