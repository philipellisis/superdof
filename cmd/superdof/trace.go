package main

import (
	"strconv"
	"strings"

	"superdof/internal/dof"
	"superdof/internal/linx"
)

// applyOutputTrace logs every change of the outputs listed in TRACE_OUTPUTS
// ("11,12,13" or "all") together with what holds them: the value of the DOF
// effect chain and the DOFLinx source keys. It is meant for finding out why an
// output stays on.
func applyOutputTrace(lg linx.Logger, cfg *linx.Config, eng *dof.Engine) {
	list := cfg.List("TRACE_OUTPUTS")
	if len(list) == 0 {
		return
	}
	all := false
	want := map[int]bool{}
	for _, p := range list {
		p = strings.TrimSpace(p)
		if strings.EqualFold(p, "all") {
			all = true
		} else if n, err := strconv.Atoi(p); err == nil {
			want[n] = true
		}
	}
	n := 0
	for _, c := range eng.Cabinet().Controllers {
		for _, o := range c.Outputs() {
			if all || want[o.Number] {
				o.SetTrace(func(m string) { lg.Infof("%s", m) })
				n++
			}
		}
	}
	for _, c := range eng.Cabinet().Controllers {
		if p, ok := c.(*dof.PinOneController); ok {
			if all {
				p.SetPacketTrace(nil, func(m string) { lg.Infof("%s", m) })
			} else {
				p.SetPacketTrace(want, func(m string) { lg.Infof("%s", m) })
			}
		}
	}
	lg.Infof("tracing changes of %d outputs (TRACE_OUTPUTS=%s)", n, strings.Join(list, ","))
}
