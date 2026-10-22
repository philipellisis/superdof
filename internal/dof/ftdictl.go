package dof

import (
	"fmt"
	"sync"
	"time"

	"superdof/internal/ftdi"
)

// FTDIController drives an FT245R bit-bang chip: 8 digital outputs, an output
// is on when its value is greater than 0 (DOF FT245RBitbangController).
type FTDIController struct {
	name    string
	serial  string
	id      int
	log     Logger
	outputs []*Output

	kick chan struct{}
	stop chan struct{}
	done chan struct{}
	run  bool
	mu   sync.Mutex

	// Open can be replaced in tests.
	Open func(serial string) (ftdi.Handle, error)
}

// NewFTDIController creates controller number id (0-9) for a chip serial.
func NewFTDIController(id int, serial string, log Logger) *FTDIController {
	if log == nil {
		log = nopLogger{}
	}
	c := &FTDIController{name: fmt.Sprintf("FT245RBitbangController %d", id), serial: serial, id: id, log: log,
		kick: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), Open: ftdi.Open}
	for i := 1; i <= 8; i++ {
		c.outputs = append(c.outputs, &Output{Name: fmt.Sprintf("%s.%02d", c.name, i), Number: i})
	}
	return c
}

// ID is the DOF controller index; its LedWizEquivalent number is ID+40.
func (c *FTDIController) ID() int { return c.id }

func (c *FTDIController) Name() string       { return c.name }
func (c *FTDIController) Outputs() []*Output { return c.outputs }
func (c *FTDIController) Notify() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

func (c *FTDIController) value() byte {
	var v byte
	for i, o := range c.outputs {
		if o.Value() > 0 {
			v |= 1 << uint(i)
		}
	}
	return v
}

func (c *FTDIController) Start() error {
	if c.serial == "" {
		return fmt.Errorf("%s: no serial number configured", c.name)
	}
	if c.run {
		return nil
	}
	c.run = true
	go c.loop()
	return nil
}

func (c *FTDIController) Stop() {
	if !c.run {
		return
	}
	c.run = false
	close(c.stop)
	<-c.done
}

func (c *FTDIController) loop() {
	defer close(c.done)
	var h ftdi.Handle
	cur := byte(255)
	lastWarn := time.Time{}
	for {
		if h == nil {
			var err error
			h, err = c.Open(c.serial)
			if err != nil {
				if time.Since(lastWarn) > 30*time.Second {
					c.log.Warnf("%s (%s): %v (will keep retrying)", c.name, c.serial, err)
					lastWarn = time.Now()
				}
				select {
				case <-c.stop:
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			c.log.Infof("%s connected (serial %s)", c.name, c.serial)
			cur = 255
			if err := h.Write(0); err != nil {
				h.Close()
				h = nil
				continue
			}
		}
		if v := c.value(); v != cur {
			if err := h.Write(v); err != nil {
				c.log.Warnf("%s: %v; reconnecting", c.name, err)
				h.Close()
				h = nil
				continue
			}
			cur = v
		}
		select {
		case <-c.stop:
			_ = h.Write(0)
			h.Close()
			return
		case <-c.kick:
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// DetectFTDI lists attached FTDI chips as controllers (DOF autoconfig).
func DetectFTDI(log Logger) []*FTDIController {
	devs, err := ftdi.List()
	if err != nil || len(devs) == 0 {
		return nil
	}
	var out []*FTDIController
	for i, d := range devs {
		if log != nil {
			log.Infof("FTDI chip %d: %s (%s)", i, d.Description, d.Serial)
		}
		out = append(out, NewFTDIController(i, d.Serial, log))
	}
	return out
}
