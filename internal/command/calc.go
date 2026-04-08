package command

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/shift-click/masterbot/internal/bot"
	"github.com/shift-click/masterbot/internal/transport"
	"github.com/shift-click/masterbot/pkg/formatter"
)

// CalcHandler evaluates arithmetic expressions as a deterministic fallback.
type CalcHandler struct {
	descriptorSupport
}

func NewCalcHandler() *CalcHandler {
	return &CalcHandler{
		descriptorSupport: newDescriptorSupport("calc"),
	}
}

// calcExprPattern pre-screens messages: digits, operators, parens, whitespace, dots only.
var calcExprPattern = regexp.MustCompile(`^[\d+\-*/%^().\s]+$`)

// stockCodePattern matches 6-digit stock codes like 005930.
var stockCodePattern = regexp.MustCompile(`^\d{6}$`)

func (h *CalcHandler) HandleFallback(ctx context.Context, cmd bot.CommandContext) error {
	msg := strings.TrimSpace(cmd.Message.Msg)
	if msg == "" {
		return nil
	}

	// Reject if contains any letters (Korean/English).
	for _, r := range msg {
		if unicode.IsLetter(r) {
			return nil
		}
	}

	// Reject pure stock code (6 digits).
	if stockCodePattern.MatchString(msg) {
		return nil
	}

	// Must match expression pattern.
	if !calcExprPattern.MatchString(msg) {
		return nil
	}

	// Must contain at least one operator.
	if !strings.ContainsAny(msg, "+-*/%^") {
		return nil
	}

	result, err := evalExpr(msg)
	if err != nil {
		return nil // parse/eval error → pass to next handler
	}

	text := formatter.FormatCalcResult(result)
	if err := cmd.Reply(ctx, bot.Reply{
		Type: transport.ReplyTypeText,
		Text: text,
	}); err != nil {
		return fmt.Errorf("calc reply: %w", err)
	}
	return bot.ErrHandled
}

// --- Pratt parser ---

type tokenKind int

const (
	tokNum tokenKind = iota
	tokPlus
	tokMinus
	tokMul
	tokDiv
	tokMod
	tokPow
	tokLParen
	tokRParen
	tokEOF
)

type token struct {
	kind tokenKind
	val  float64
}

type lexer struct {
	input []rune
	pos   int
}

func newLexer(s string) *lexer {
	return &lexer{input: []rune(s)}
}

func (l *lexer) next() (token, error) {
	l.skipSpaces()
	if l.pos >= len(l.input) {
		return token{kind: tokEOF}, nil
	}

	ch := l.input[l.pos]
	switch ch {
	case '+':
		l.pos++
		return token{kind: tokPlus}, nil
	case '-':
		l.pos++
		return token{kind: tokMinus}, nil
	case '*':
		l.pos++
		return token{kind: tokMul}, nil
	case '/':
		l.pos++
		return token{kind: tokDiv}, nil
	case '%':
		l.pos++
		return token{kind: tokMod}, nil
	case '^':
		l.pos++
		return token{kind: tokPow}, nil
	case '(':
		l.pos++
		return token{kind: tokLParen}, nil
	case ')':
		l.pos++
		return token{kind: tokRParen}, nil
	}

	if ch >= '0' && ch <= '9' || ch == '.' {
		return l.readNumber()
	}

	return token{}, fmt.Errorf("unexpected character: %c", ch)
}

func (l *lexer) readNumber() (token, error) {
	start := l.pos
	hasDot := false
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == '.' {
			if hasDot {
				break
			}
			hasDot = true
			l.pos++
		} else if ch >= '0' && ch <= '9' {
			l.pos++
		} else {
			break
		}
	}
	s := string(l.input[start:l.pos])
	var val float64
	_, err := fmt.Sscanf(s, "%f", &val)
	if err != nil {
		return token{}, fmt.Errorf("invalid number: %s", s)
	}
	return token{kind: tokNum, val: val}, nil
}

func (l *lexer) skipSpaces() {
	for l.pos < len(l.input) && l.input[l.pos] == ' ' {
		l.pos++
	}
}

type parser struct {
	lex     *lexer
	current token
}

func newParser(input string) (*parser, error) {
	lex := newLexer(input)
	tok, err := lex.next()
	if err != nil {
		return nil, err
	}
	return &parser{lex: lex, current: tok}, nil
}

func (p *parser) advance() error {
	tok, err := p.lex.next()
	if err != nil {
		return err
	}
	p.current = tok
	return nil
}

func (p *parser) parseExpr(minBP int) (float64, error) {
	lhs, err := p.parsePrefix()
	if err != nil {
		return 0, err
	}

	for {
		bp := infixBP(p.current.kind)
		if bp <= minBP {
			break
		}

		op := p.current.kind
		if err := p.advance(); err != nil {
			return 0, err
		}

		rhs, err := p.parseExpr(nextBindingPower(op, bp))
		if err != nil {
			return 0, err
		}
		lhs, err = applyInfix(op, lhs, rhs)
		if err != nil {
			return 0, err
		}
	}

	return lhs, nil
}

func (p *parser) parsePrefix() (float64, error) {
	switch p.current.kind {
	case tokNum:
		return p.parseNumber()
	case tokMinus:
		return p.parseUnary(-1)
	case tokPlus:
		return p.parseUnary(1)
	case tokLParen:
		return p.parseParenthesized()
	default:
		return 0, fmt.Errorf("unexpected token")
	}
}

func nextBindingPower(op tokenKind, bp int) int {
	if op == tokPow {
		return bp - 1
	}
	return bp
}

func applyInfix(op tokenKind, lhs, rhs float64) (float64, error) {
	switch op {
	case tokPlus:
		return lhs + rhs, nil
	case tokMinus:
		return lhs - rhs, nil
	case tokMul:
		return lhs * rhs, nil
	case tokDiv:
		if rhs == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return lhs / rhs, nil
	case tokMod:
		if rhs == 0 {
			return 0, fmt.Errorf("modulo by zero")
		}
		return math.Mod(lhs, rhs), nil
	case tokPow:
		return math.Pow(lhs, rhs), nil
	default:
		return 0, fmt.Errorf("unexpected operator")
	}
}

func (p *parser) parseNumber() (float64, error) {
	val := p.current.val
	if err := p.advance(); err != nil {
		return 0, err
	}
	return val, nil
}

func (p *parser) parseUnary(sign float64) (float64, error) {
	if err := p.advance(); err != nil {
		return 0, err
	}
	val, err := p.parseExpr(100)
	if err != nil {
		return 0, err
	}
	return sign * val, nil
}

func (p *parser) parseParenthesized() (float64, error) {
	if err := p.advance(); err != nil {
		return 0, err
	}
	val, err := p.parseExpr(0)
	if err != nil {
		return 0, err
	}
	if p.current.kind != tokRParen {
		return 0, fmt.Errorf("expected closing parenthesis")
	}
	if err := p.advance(); err != nil {
		return 0, err
	}
	return val, nil
}

func infixBP(kind tokenKind) int {
	switch kind {
	case tokPlus, tokMinus:
		return 10
	case tokMul, tokDiv, tokMod:
		return 20
	case tokPow:
		return 30
	default:
		return 0
	}
}

// evalExpr evaluates a mathematical expression string.
func evalExpr(input string) (float64, error) {
	p, err := newParser(input)
	if err != nil {
		return 0, err
	}
	result, err := p.parseExpr(0)
	if err != nil {
		return 0, err
	}
	if p.current.kind != tokEOF {
		return 0, fmt.Errorf("unexpected trailing input")
	}
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return 0, fmt.Errorf("result is not finite")
	}
	return result, nil
}
