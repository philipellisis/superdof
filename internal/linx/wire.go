package linx

import (
	"strconv"
	"strings"
)

// TriggerMessage is a decoded message of DOFLinxTrigger.dll.
//
// The DLL (loaded by Pinball FX / Pinball M / Infected Mushroom Pinball as
// a cabinet plugin) writes obfuscated text records to the named pipe
// "DOFLinxTrigger": a 0x05 header byte, the text with every byte reduced by
// 10 + index/7, and a CR LF terminator. The text is "<K>,<arg>[,<value>]":
//
//	E,<EVENT>,<value>   table event (BUMPER01, Flipper_Left, ...)
//	G,<id>              game (table) changed
//	S,<status>          Menu, InLane, Play or a number
//	N,<dir>             nudge: Left, Right, Forward
//	C,<player>          score changed
type TriggerMessage struct {
	Kind  byte // 'E','G','S','N','C'
	Arg   string
	Value int
	HasV  bool
	Raw   string
}

// DecodeTrigger decodes one record (without the CR LF terminator and
// without the leading 0x05 header byte).
func DecodeTrigger(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c + byte(10+i/7)
	}
	return string(out)
}

// EncodeTrigger is the inverse of DecodeTrigger (tests and tooling).
func EncodeTrigger(text string) []byte {
	out := []byte{0x05}
	for i := 0; i < len(text); i++ {
		out = append(out, text[i]-byte(10+i/7))
	}
	return append(out, '\r', '\n')
}

// SplitTriggerRecords splits a byte stream into decoded text records.
// Records are separated by CR LF; the leading 0x05 of each is removed.
func SplitTriggerRecords(stream []byte, carry *[]byte) []string {
	buf := append(*carry, stream...)
	var out []string
	for {
		i := -1
		for j := 0; j+1 < len(buf); j++ {
			if buf[j] == '\r' && buf[j+1] == '\n' {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		rec := buf[:i]
		buf = buf[i+2:]
		for len(rec) > 0 && rec[0] == 0x05 {
			rec = rec[1:]
		}
		if len(rec) > 0 {
			out = append(out, DecodeTrigger(rec))
		}
	}
	*carry = append((*carry)[:0], buf...)
	return out
}

// ParseTrigger parses decoded text such as "E,BUMPER01,1".
func ParseTrigger(text string) (TriggerMessage, bool) {
	text = strings.TrimSpace(text)
	if len(text) < 3 || text[1] != ',' {
		return TriggerMessage{}, false
	}
	m := TriggerMessage{Kind: text[0], Raw: text}
	rest := text[2:]
	if m.Kind == 'E' {
		if i := strings.LastIndexByte(rest, ','); i > 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(rest[i+1:])); err == nil {
				m.Arg, m.Value, m.HasV = rest[:i], n, true
				return m, true
			}
		}
	}
	m.Arg = rest
	return m, true
}
