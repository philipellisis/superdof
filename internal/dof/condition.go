package dof

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// condExpr is a compiled ledcontrol condition such as
// (E229=1 OR E220=1) AND NOT W8=0. Identifiers are table elements and
// evaluate to the current element value.
type condExpr struct {
	root condNode
	vars []string // element descriptors referenced, upper case ("E229", "$NAME")
}

type condNode interface {
	eval(r func(string) float64) float64
}

type condNum float64

func (n condNum) eval(func(string) float64) float64 { return float64(n) }

type condVar string

func (v condVar) eval(r func(string) float64) float64 { return r(string(v)) }

type condUn struct {
	op string
	x  condNode
}

func (u condUn) eval(r func(string) float64) float64 {
	x := u.x.eval(r)
	switch u.op {
	case "NOT":
		return b2f(x == 0)
	case "-":
		return -x
	}
	return x
}

type condBin struct {
	op   string
	l, r condNode
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func (b condBin) eval(r func(string) float64) float64 {
	switch b.op {
	case "AND":
		if b.l.eval(r) == 0 {
			return 0
		}
		return b2f(b.r.eval(r) != 0)
	case "OR":
		if b.l.eval(r) != 0 {
			return 1
		}
		return b2f(b.r.eval(r) != 0)
	}
	l, rr := b.l.eval(r), b.r.eval(r)
	switch b.op {
	case "XOR":
		return b2f((l != 0) != (rr != 0))
	case "=", "==":
		return b2f(l == rr)
	case "<>", "!=":
		return b2f(l != rr)
	case "<":
		return b2f(l < rr)
	case ">":
		return b2f(l > rr)
	case "<=":
		return b2f(l <= rr)
	case ">=":
		return b2f(l >= rr)
	case "+":
		return l + rr
	case "-":
		return l - rr
	case "*":
		return l * rr
	case "/":
		if rr == 0 {
			return 0
		}
		return l / rr
	case "%":
		if rr == 0 {
			return 0
		}
		return math.Mod(l, rr)
	}
	return 0
}

type condParser struct {
	toks []string
	pos  int
	vars map[string]bool
	list []string
}

func tokenize(s string) ([]string, error) {
	var out []string
	rs := []rune(s)
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '(' || c == ')' || c == '+' || c == '-' || c == '*' || c == '/' || c == '%':
			out = append(out, string(c))
			i++
		case c == '&' || c == '|':
			j := i + 1
			if j < len(rs) && rs[j] == c {
				j++
			}
			if c == '&' {
				out = append(out, "AND")
			} else {
				out = append(out, "OR")
			}
			i = j
		case c == '!' && i+1 < len(rs) && rs[i+1] == '=':
			out = append(out, "<>")
			i += 2
		case c == '!':
			out = append(out, "NOT")
			i++
		case c == '=' || c == '<' || c == '>':
			j := i + 1
			if j < len(rs) && (rs[j] == '=' || (c == '<' && rs[j] == '>')) {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
		case unicode.IsDigit(c) || c == '.':
			j := i
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.') {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
		case unicode.IsLetter(c) || c == '_' || c == '$':
			j := i + 1
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			out = append(out, string(rs[i:j]))
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q in condition", c)
		}
	}
	return out, nil
}

func (p *condParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *condParser) next() string { t := p.peek(); p.pos++; return t }

func (p *condParser) parseOr() (condNode, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "OR" || p.peek() == "XOR" {
		op := p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = condBin{op, l, r}
	}
	return l, nil
}

func (p *condParser) parseAnd() (condNode, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek() == "AND" {
		p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = condBin{"AND", l, r}
	}
	return l, nil
}

func (p *condParser) parseNot() (condNode, error) {
	if p.peek() == "NOT" {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return condUn{"NOT", x}, nil
	}
	return p.parseCmp()
}

func (p *condParser) parseCmp() (condNode, error) {
	l, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek() {
		case "=", "==", "<>", "<", ">", "<=", ">=":
			op := p.next()
			r, err := p.parseAdd()
			if err != nil {
				return nil, err
			}
			l = condBin{op, l, r}
		default:
			return l, nil
		}
	}
}

func (p *condParser) parseAdd() (condNode, error) {
	l, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.peek() == "+" || p.peek() == "-" {
		op := p.next()
		r, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		l = condBin{op, l, r}
	}
	return l, nil
}

func (p *condParser) parseMul() (condNode, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek() == "*" || p.peek() == "/" || p.peek() == "%" {
		op := p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = condBin{op, l, r}
	}
	return l, nil
}

func (p *condParser) parseUnary() (condNode, error) {
	switch p.peek() {
	case "-":
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return condUn{"-", x}, nil
	case "(":
		p.next()
		x, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.next() != ")" {
			return nil, fmt.Errorf("missing closing bracket")
		}
		return x, nil
	case "":
		return nil, fmt.Errorf("unexpected end of condition")
	}
	t := p.next()
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return condNum(f), nil
	}
	up := strings.ToUpper(t)
	switch up {
	case "TRUE":
		return condNum(1), nil
	case "FALSE":
		return condNum(0), nil
	}
	if !p.vars[up] {
		p.vars[up] = true
		p.list = append(p.list, up)
	}
	return condVar(up), nil
}

func compileCondition(s string) (*condExpr, error) {
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	p := &condParser{toks: toks, vars: map[string]bool{}}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.toks) {
		return nil, fmt.Errorf("unexpected token %q", p.toks[p.pos])
	}
	return &condExpr{root: n, vars: p.list}, nil
}

// conditionEffect evaluates a condition on every trigger of a referenced
// element and forwards 255/0 to its target effect.
type conditionEffect struct {
	chainEffect
	condition string
	expr      *condExpr
	elems     map[string]*TableElement
}

func (c *conditionEffect) Init(t *Table) {
	c.chainEffect.Init(t)
	expr, err := compileCondition(c.condition)
	if err != nil {
		t.engine.log.Warnf("cannot compile condition %s of %s: %v", c.condition, c.name, err)
		return
	}
	c.expr = expr
	c.elems = map[string]*TableElement{}
	for _, v := range expr.vars {
		c.elems[v] = t.element(NewTED(v, 0))
	}
}

func (c *conditionEffect) Trigger(d TED) {
	if c.expr == nil {
		return
	}
	v := c.expr.root.eval(func(name string) float64 {
		if e, ok := c.elems[name]; ok && e.Value != math.MinInt32 {
			return float64(e.Value)
		}
		return 0
	})
	if v != 0 {
		d.Value = 255
	} else {
		d.Value = 0
	}
	c.fire(d)
}
