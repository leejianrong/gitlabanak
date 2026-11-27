package ci

import (
	"fmt"
	"regexp"
	"strings"
)

// Expr is a compiled rules:if expression. It supports $VAR, "strings", /regex/ and null, the
// operators == != =~ !~, && || and parentheses.
type Expr func(vars map[string]string) bool

// Compile parses a rules:if expression.
func Compile(src string) (Expr, error) {
	p := &parser{src: src}
	e, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.skip(); p.pos != len(p.src) {
		return nil, fmt.Errorf("unexpected %q", p.src[p.pos:])
	}
	return e, nil
}

type parser struct {
	src string
	pos int
}

func (p *parser) skip() {
	for p.pos < len(p.src) && p.src[p.pos] == ' ' {
		p.pos++
	}
}

func (p *parser) eat(tok string) bool {
	p.skip()
	if strings.HasPrefix(p.src[p.pos:], tok) {
		p.pos += len(tok)
		return true
	}
	return false
}

func (p *parser) or() (Expr, error) {
	l, err := p.and()
	for err == nil && p.eat("||") {
		var r Expr
		if r, err = p.and(); err == nil {
			a, b := l, r
			l = func(v map[string]string) bool { return a(v) || b(v) }
		}
	}
	return l, err
}

func (p *parser) and() (Expr, error) {
	l, err := p.cmp()
	for err == nil && p.eat("&&") {
		var r Expr
		if r, err = p.cmp(); err == nil {
			a, b := l, r
			l = func(v map[string]string) bool { return a(v) && b(v) }
		}
	}
	return l, err
}

// operand is a value that is either a variable, a literal, or a regex.
type operand struct {
	value func(map[string]string) (string, bool) // the string and whether it is set
	re    *regexp.Regexp
}

func (p *parser) cmp() (Expr, error) {
	if p.eat("(") {
		e, err := p.or()
		if err != nil || !p.eat(")") {
			return nil, fmt.Errorf("missing )")
		}
		return e, nil
	}
	l, err := p.operand()
	if err != nil {
		return nil, err
	}
	if l.value == nil {
		return nil, fmt.Errorf("a /regex/ must be on the right of =~ or !~")
	}
	var op string
	for _, c := range []string{"==", "!=", "=~", "!~"} {
		if p.eat(c) {
			op = c
			break
		}
	}
	if op == "" {
		return func(v map[string]string) bool { s, ok := l.value(v); return ok && s != "" }, nil
	}
	r, err := p.operand()
	if err != nil {
		return nil, err
	}
	switch op {
	case "==", "!=":
		if r.value == nil {
			return nil, fmt.Errorf("%s cannot compare with a /regex/", op)
		}
		return func(v map[string]string) bool {
			ls, lok := l.value(v)
			rs, rok := r.value(v)
			eq := lok == rok && ls == rs
			return eq == (op == "==")
		}, nil
	default:
		if r.re == nil {
			return nil, fmt.Errorf("%s needs a /regex/ on the right", op)
		}
		return func(v map[string]string) bool {
			s, ok := l.value(v)
			return ok && r.re.MatchString(s) == (op == "=~")
		}, nil
	}
}

func (p *parser) operand() (operand, error) {
	p.skip()
	if p.pos >= len(p.src) {
		return operand{}, fmt.Errorf("unexpected end")
	}
	switch c := p.src[p.pos]; {
	case c == '$':
		start := p.pos + 1
		end := start
		for end < len(p.src) && (p.src[end] == '_' || p.src[end] >= '0' && p.src[end] <= '9' || p.src[end]|0x20 >= 'a' && p.src[end]|0x20 <= 'z') {
			end++
		}
		if end == start {
			return operand{}, fmt.Errorf("empty variable name")
		}
		name := p.src[start:end]
		p.pos = end
		return operand{value: func(v map[string]string) (string, bool) { s, ok := v[name]; return s, ok }}, nil
	case c == '"' || c == '\'':
		end := strings.IndexByte(p.src[p.pos+1:], c)
		if end < 0 {
			return operand{}, fmt.Errorf("unterminated string")
		}
		lit := p.src[p.pos+1 : p.pos+1+end]
		p.pos += end + 2
		return operand{value: func(map[string]string) (string, bool) { return lit, true }}, nil
	case c == '/':
		end := p.pos + 1
		for end < len(p.src) && p.src[end] != '/' {
			if p.src[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(p.src) {
			return operand{}, fmt.Errorf("unterminated regex")
		}
		pattern := p.src[p.pos+1 : end]
		p.pos = end + 1
		if p.pos < len(p.src) && p.src[p.pos] == 'i' {
			pattern, p.pos = "(?i)"+pattern, p.pos+1
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return operand{}, err
		}
		return operand{re: re}, nil
	case strings.HasPrefix(p.src[p.pos:], "null"):
		p.pos += 4
		return operand{value: func(map[string]string) (string, bool) { return "", false }}, nil
	}
	return operand{}, fmt.Errorf("unexpected %q", p.src[p.pos:])
}
