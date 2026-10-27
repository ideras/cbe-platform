package calc

import (
	"math/big"
	"slices"
)

// The programmer lexer, parser and evaluator. The grammar is C-like, without the scientific mode's implicit
// multiplication and postfix !: precedence from low to high is | ^ & shifts + - * / %, with unary - and ~ above
// all, so `1 + 2 << 3` is 24 and `1 | 2 ^ 3` is 1. In the input base HEX a bare word made of hex digits is a
// literal (`ff` is 255); a name is only a function when `(` follows it.
//
// Values are integers or reals, C-style, by how they are written (D20): a literal with a point is a real, even
// 300.0, held as an exact rational; + - * / and unary - give a real when an operand is real, the other operators
// are integer-only. A point literal is decimal: in the DEC input base, or after 0d in any base (D19).

type intTokenKind int

const (
	intEOF intTokenKind = iota
	intNumber
	intIdent
	intOperator
)

type intToken struct {
	kind  intTokenKind
	text  string
	value *big.Int // an integer literal
	real  *big.Rat // a point literal
}

func lexInt(input string, base int) ([]intToken, *Error) {
	var tokens []intToken
	for i := 0; i < len(input); {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isIntWordChar(c):
			end := i + 1
			for end < len(input) && isIntWordChar(input[end]) {
				end++
			}
			word := input[i:end]
			if c >= '0' && c <= '9' && end < len(input) && input[end] == '.' {
				// A point literal: the digits before the point and the word after it are both decimal digits.
				real, next, ok := realLiteral(input, word, end, base)
				if !ok {
					return nil, &Error{Code: CodeSyntax}
				}
				tokens = append(tokens, intToken{kind: intNumber, real: real})
				i = next
				continue
			}
			if c >= '0' && c <= '9' || base == 16 && isHexWord(word) {
				// A word that starts with a digit is a literal; in HEX a bare word of hex digits is one too.
				value, ok := intLiteral(word, base)
				if !ok {
					return nil, &Error{Code: CodeSyntax}
				}
				tokens = append(tokens, intToken{kind: intNumber, value: value})
			} else {
				tokens = append(tokens, intToken{kind: intIdent, text: word})
			}
			i = end
		default:
			kind, ok := scanIntOperator(input, i)
			if !ok {
				return nil, &Error{Code: CodeSyntax}
			}
			tokens = append(tokens, intToken{kind: intOperator, text: kind})
			i += len(kind)
		}
	}
	return tokens, nil
}

// realLiteral reads `digits.digits` (D19) from the word before the point at input[point]. The point belongs to a
// decimal number only: the word is unprefixed in the DEC input base, or carries 0d in any base; a 0x, 0b or 0o
// literal never takes one. The fraction is one word of decimal digits, so 1.5e3, 1. and 1.2.3 are refused,
// and there is no exponent. It answers the value and the index after the literal.
func realLiteral(input, word string, point, base int) (*big.Rat, int, bool) {
	whole := word
	if len(word) > 1 && word[0] == '0' && (word[1] == 'd' || word[1] == 'D') {
		whole = word[2:]
	} else if base != 10 {
		return nil, 0, false
	}
	end := point + 1
	for end < len(input) && isIntWordChar(input[end]) {
		end++
	}
	fraction := input[point+1 : end]
	if !allDecimal(whole) || !allDecimal(fraction) {
		return nil, 0, false
	}
	value, ok := new(big.Rat).SetString(whole + "." + fraction)
	return value, end, ok
}

func allDecimal(word string) bool { return allDigits(word, 10) }

func isIntWordChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isHexWord(word string) bool {
	if word == "" {
		return false
	}
	for i := 0; i < len(word); i++ {
		c := word[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// intLiteral reads a literal: an explicit prefix wins (0x, 0b, 0o, 0d, case-insensitive on the prefix letter),
// otherwise the input base decides. It answers false when the word is not a literal in any reading.
func intLiteral(word string, base int) (*big.Int, bool) {
	value := new(big.Int)
	if len(word) > 1 && word[0] == '0' {
		prefixBase := 0
		switch word[1] {
		case 'x', 'X':
			prefixBase = 16
		case 'b', 'B':
			prefixBase = 2
		case 'o', 'O':
			prefixBase = 8
		case 'd', 'D':
			prefixBase = 10
		}
		if prefixBase != 0 {
			digits := word[2:]
			if digits == "" || !allDigits(digits, prefixBase) {
				return nil, false
			}
			if _, ok := value.SetString(digits, prefixBase); !ok {
				return nil, false
			}
			return value, true
		}
	}
	if !allDigits(word, base) {
		return nil, false
	}
	if _, ok := value.SetString(word, base); !ok {
		return nil, false
	}
	return value, true
}

func allDigits(word string, base int) bool {
	if word == "" {
		return false
	}
	for i := 0; i < len(word); i++ {
		switch c := word[i]; {
		case c >= '0' && c <= '9':
		case base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'):
		default:
			return false
		}
	}
	return true
}

func scanIntOperator(input string, i int) (string, bool) {
	if i+1 < len(input) {
		switch input[i : i+2] {
		case "<<", ">>":
			return input[i : i+2], true
		}
	}
	switch input[i] {
	case '+', '-', '*', '/', '%', '&', '|', '^', '~', '(', ')', ',':
		return input[i : i+1], true
	}
	return "", false
}

// intNode is the programmer tree: a literal, ans, a unary, a binary operator, or a call. The node kinds are
// shared with the scientific tree.
type intNode struct {
	kind     nodeKind // nodeNumber, nodeIdent, nodeUnary, nodeBinary, nodeCall
	text     string   // the operator, or the identifier/callee name
	value    *big.Int // an integer literal's value
	real     *big.Rat // a point literal's value
	argv     []*intNode
	operator string
}

type intEval struct {
	opts        IntOptions
	word        int
	mask        *big.Int // 2^N - 1
	rangeTop    *big.Int // 2^(N-1) - 1
	rangeBottom *big.Int // -2^(N-1)
	fitCode     string   // an integer literal too wide for the word: OVERFLOW, or BIT_WIDTH in the inspector's Decode
}

// maxRealBits bounds a real's numerator and denominator, so `ans * ans` repeated cannot grow without limit. It is
// far beyond what a 200-character input writes (about 665 bits) and than binary64's range (2^1024, 2^-1074).
const maxRealBits = 4096

// pval is one programmer value: an integer, its masked N-bit value, or a real, an exact rational (D20). A real
// zero carries its sign, as in IEEE 754, so -0.0 encodes as negative zero.
type pval struct {
	i        *big.Int
	r        *big.Rat
	negative bool // a real zero is -0
}

func (v pval) isReal() bool { return v.r != nil }

func (e *intEval) parse(tokens []intToken) (*intNode, *Error) {
	p := &intParser{tokens: tokens, e: e}
	root, err := p.bitor()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != intEOF {
		return nil, &Error{Code: CodeSyntax}
	}
	return root, nil
}

type intParser struct {
	tokens []intToken
	pos    int
	e      *intEval
	depth  int
}

func (p *intParser) peek() intToken {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return intToken{kind: intEOF}
}

func (p *intParser) next() intToken {
	t := p.peek()
	if t.kind != intEOF {
		p.pos++
	}
	return t
}

// chain parses one left-associative precedence level: operands from the next tighter level, joined by any of
// the level's operators.
func (p *intParser) chain(tighter func() (*intNode, *Error), operators ...string) (*intNode, *Error) {
	left, err := tighter()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op.kind != intOperator || !slices.Contains(operators, op.text) {
			return left, nil
		}
		p.next()
		right, err := tighter()
		if err != nil {
			return nil, err
		}
		left = &intNode{kind: nodeBinary, operator: op.text, argv: []*intNode{left, right}}
	}
}

func (p *intParser) bitor() (*intNode, *Error)  { return p.chain(p.bitxor, "|") }
func (p *intParser) bitxor() (*intNode, *Error) { return p.chain(p.bitand, "^") }
func (p *intParser) bitand() (*intNode, *Error) { return p.chain(p.shift, "&") }
func (p *intParser) shift() (*intNode, *Error)  { return p.chain(p.add, "<<", ">>") }
func (p *intParser) add() (*intNode, *Error)    { return p.chain(p.mul, "+", "-") }
func (p *intParser) mul() (*intNode, *Error)    { return p.chain(p.unary, "*", "/", "%") }

func (p *intParser) unary() (*intNode, *Error) {
	switch kind := p.peek(); kind.kind {
	case intOperator:
		switch kind.text {
		case "-", "~":
			p.next()
			operand, err := p.unary()
			if err != nil {
				return nil, err
			}
			return &intNode{kind: nodeUnary, operator: kind.text, argv: []*intNode{operand}}, nil
		}
	}
	return p.primary()
}

func (p *intParser) primary() (*intNode, *Error) {
	t := p.next()
	switch t.kind {
	case intNumber:
		return &intNode{kind: nodeNumber, value: t.value, real: t.real}, nil
	case intIdent:
		if p.peek().kind == intOperator && p.peek().text == "(" {
			p.next()
			p.depth++
			if p.depth > maxNesting {
				return nil, &Error{Code: CodeTooDeep}
			}
			var argv []*intNode
			if !(p.peek().kind == intOperator && p.peek().text == ")") {
				for {
					argument, err := p.bitor()
					if err != nil {
						return nil, err
					}
					argv = append(argv, argument)
					if !(p.peek().kind == intOperator && p.peek().text == ",") {
						break
					}
					p.next()
				}
			}
			if closing := p.next(); !(closing.kind == intOperator && closing.text == ")") {
				return nil, &Error{Code: CodeSyntax}
			}
			p.depth--
			return &intNode{kind: nodeCall, text: t.text, argv: argv}, nil
		}
		return &intNode{kind: nodeIdent, text: t.text}, nil
	case intOperator:
		if t.text == "(" {
			p.depth++
			if p.depth > maxNesting {
				return nil, &Error{Code: CodeTooDeep}
			}
			inner, err := p.bitor()
			if err != nil {
				return nil, err
			}
			if closing := p.next(); !(closing.kind == intOperator && closing.text == ")") {
				return nil, &Error{Code: CodeSyntax}
			}
			p.depth--
			return inner, nil
		}
	}
	return nil, &Error{Code: CodeSyntax}
}

// eval computes a node: the value plus the flags of the operation it applies, which only the root of the tree
// reports (the flags of the last operator evaluated, like a CPU's flags after its last instruction). A real sets
// no flags.
func (e *intEval) eval(n *intNode) (pval, bool, bool, *Error) {
	switch n.kind {
	case nodeNumber:
		if n.real != nil {
			return pval{r: new(big.Rat).Set(n.real)}, false, false, nil
		}
		return e.literal(n.value)
	case nodeIdent:
		return e.ident(n.text)
	case nodeUnary:
		value, _, _, err := e.eval(n.argv[0])
		if err != nil {
			return pval{}, false, false, err
		}
		switch n.operator {
		case "-":
			if value.isReal() {
				return pval{r: new(big.Rat).Neg(value.r), negative: value.r.Sign() == 0 && !value.negative}, false, false, nil
			}
			return pval{i: new(big.Int).And(new(big.Int).Neg(value.i), e.mask)}, false, false, nil
		case "~":
			if value.isReal() {
				return pval{}, false, false, &Error{Code: CodeNotInteger}
			}
			return pval{i: new(big.Int).And(new(big.Int).Not(value.i), e.mask)}, false, false, nil
		}
	case nodeBinary:
		left, _, _, err := e.eval(n.argv[0])
		if err != nil {
			return pval{}, false, false, err
		}
		right, _, _, err := e.eval(n.argv[1])
		if err != nil {
			return pval{}, false, false, err
		}
		return e.binary(n.operator, left, right)
	case nodeCall:
		return e.call(n)
	}
	return pval{}, false, false, &Error{Code: CodeSyntax}
}

// ident answers `ans` or a bare literal in the input base; anything else is an unknown identifier, whether or
// not a function of that name exists. An integer ans is read at the current word size.
func (e *intEval) ident(name string) (pval, bool, bool, *Error) {
	if name == "ans" {
		if e.opts.Ans.Real != nil {
			return pval{r: new(big.Rat).Set(e.opts.Ans.Real), negative: e.opts.Ans.NegativeZero}, false, false, nil
		}
		return pval{i: new(big.Int).And(new(big.Int).SetUint64(e.opts.Ans.Bits), e.mask)}, false, false, nil
	}
	value, ok := intLiteral(name, e.opts.Base)
	if !ok {
		return pval{}, false, false, &Error{Code: CodeUnknownIdent}
	}
	return e.literal(value)
}

// literal applies an integer literal's fit rule: 0 ≤ value < 2^N; a negative number is written with unary -.
func (e *intEval) literal(value *big.Int) (pval, bool, bool, *Error) {
	if value.Sign() < 0 || value.Cmp(new(big.Int).Add(e.mask, big.NewInt(1))) >= 0 {
		return pval{}, false, false, &Error{Code: e.fitCode}
	}
	return pval{i: new(big.Int).Set(value)}, false, false, nil
}

func (e *intEval) call(n *intNode) (pval, bool, bool, *Error) {
	if !e.opts.Allowed[n.text] {
		return pval{}, false, false, &Error{Code: CodeUnknownFunction}
	}
	entry, ok := Registry[n.text]
	if !ok || entry.Pseudo {
		return pval{}, false, false, &Error{Code: CodeUnknownFunction}
	}
	if len(n.argv) < entry.MinArgs || len(n.argv) > entry.MaxArgs {
		return pval{}, false, false, &Error{Code: CodeArity}
	}
	arguments := make([]*big.Int, len(n.argv))
	for i, argument := range n.argv {
		value, _, _, err := e.eval(argument)
		if err != nil {
			return pval{}, false, false, err
		}
		if value.isReal() {
			return pval{}, false, false, &Error{Code: CodeNotInteger} // the programmer functions are integer-only (D20)
		}
		arguments[i] = value.i
	}
	value, carry, overflow, err := e.function(n.text, arguments)
	return pval{i: value}, carry, overflow, err
}

// binary computes a binary operator. With a real operand, + - * / are real arithmetic, C's usual arithmetic
// conversions, and every other operator is NOT_INTEGER (D20).
func (e *intEval) binary(operator string, a, b pval) (pval, bool, bool, *Error) {
	if a.isReal() || b.isReal() {
		switch operator {
		case "+", "-", "*", "/":
			return e.realArithmetic(operator, a, b)
		}
		return pval{}, false, false, &Error{Code: CodeNotInteger}
	}
	value, carry, overflow, err := e.integer(operator, a.i, b.i)
	return pval{i: value}, carry, overflow, err
}

// realArithmetic computes + - * / exactly. A zero result takes IEEE 754's sign under round-to-nearest: a product
// or quotient is -0 when the signs differ, (-0) + (-0) and (-0) - (+0) are -0, and x + (-x) is +0.
func (e *intEval) realArithmetic(operator string, a, b pval) (pval, bool, bool, *Error) {
	x, y := e.toReal(a), e.toReal(b)
	result := new(big.Rat)
	switch operator {
	case "+":
		result.Add(x, y)
	case "-":
		result.Sub(x, y)
	case "*":
		result.Mul(x, y)
	case "/":
		if y.Sign() == 0 {
			return pval{}, false, false, &Error{Code: CodeDivisionByZero}
		}
		result.Quo(x, y)
	}
	if result.Num().BitLen() > maxRealBits || result.Denom().BitLen() > maxRealBits {
		return pval{}, false, false, &Error{Code: CodeOverflow}
	}
	negative := false
	if result.Sign() == 0 {
		switch operator {
		case "*", "/":
			negative = e.negative(a) != e.negative(b)
		case "+":
			negative = e.negative(a) && e.negative(b)
		case "-":
			negative = e.negative(a) && !e.negative(b)
		}
	}
	return pval{r: result, negative: negative}, false, false, nil
}

// toReal reads a value as a rational: a real as it is, an integer through the signed/unsigned toggle, so 8-bit
// 0xFF is -1 when signed and 255 when unsigned.
func (e *intEval) toReal(v pval) *big.Rat {
	if v.isReal() {
		return v.r
	}
	if e.opts.Signed {
		return new(big.Rat).SetInt(e.signed(v.i))
	}
	return new(big.Rat).SetInt(v.i)
}

// negative is a value's sign bit: a negative value, or a real -0.
func (e *intEval) negative(v pval) bool {
	sign := e.toReal(v).Sign()
	return sign < 0 || sign == 0 && v.isReal() && v.negative
}

func (e *intEval) function(name string, x []*big.Int) (*big.Int, bool, bool, *Error) {
	switch name {
	case "nand":
		return e.logic(x[0], x[1], func(a, b *big.Int) *big.Int { return new(big.Int).Not(new(big.Int).And(a, b)) })
	case "nor":
		return e.logic(x[0], x[1], func(a, b *big.Int) *big.Int { return new(big.Int).Not(new(big.Int).Or(a, b)) })
	case "rol", "ror":
		count, err := e.rotateCount(x[1])
		if err != nil {
			return nil, false, false, err
		}
		countU := uint(count.Int64())
		left, right := new(big.Int).Lsh(x[0], countU), new(big.Int).Rsh(x[0], uint(e.word)-countU)
		if name == "ror" {
			left, right = new(big.Int).Rsh(x[0], countU), new(big.Int).Lsh(x[0], uint(e.word)-countU)
		}
		return new(big.Int).And(new(big.Int).Or(left, right), e.mask), false, false, nil
	}
	return nil, false, false, &Error{Code: CodeUnknownFunction}
}

// logic computes a bitwise function call; flags are 0 like every bitwise operation's.
func (e *intEval) logic(a, b *big.Int, combine func(*big.Int, *big.Int) *big.Int) (*big.Int, bool, bool, *Error) {
	return new(big.Int).And(combine(a, b), e.mask), false, false, nil
}

// rotateCount applies the range rule the shift counts follow (D15): a count outside [0, N-1] is SHIFT_RANGE,
// including a negative count, which is any pattern whose unsigned reading is ≥ N.
func (e *intEval) rotateCount(value *big.Int) (*big.Int, *Error) {
	return e.shiftCount(value)
}

func (e *intEval) shiftCount(value *big.Int) (*big.Int, *Error) {
	if value.Sign() < 0 || value.Cmp(big.NewInt(int64(e.word))) >= 0 {
		return nil, &Error{Code: CodeShiftRange}
	}
	return value, nil
}

// integer computes an operator on two integers: the masked N-bit value and the operation's flags.
func (e *intEval) integer(operator string, a, b *big.Int) (*big.Int, bool, bool, *Error) {
	switch operator {
	case "+", "-", "*":
		// CF is the carry out (unsigned result ≥ 2^N) or the borrow (unsigned a < b); OF is the signed
		// computation — the operands read as two's complement — leaving the N-bit range. Both readings come from
		// the same values; the toggle never changes either (D15).
		unsigned := new(big.Int)
		var carry bool
		switch operator {
		case "+":
			unsigned.Add(a, b)
			carry = unsigned.Cmp(new(big.Int).Lsh(big.NewInt(1), uint(e.word))) >= 0
		case "-":
			unsigned.Sub(a, b)
			carry = a.Cmp(b) < 0
		case "*":
			unsigned.Mul(a, b)
			carry = unsigned.Cmp(new(big.Int).Lsh(big.NewInt(1), uint(e.word))) >= 0
		}
		signedSum := new(big.Int)
		switch operator {
		case "+":
			signedSum.Add(e.signed(a), e.signed(b))
		case "-":
			signedSum.Sub(e.signed(a), e.signed(b))
		case "*":
			signedSum.Mul(e.signed(a), e.signed(b))
		}
		return new(big.Int).And(unsigned, e.mask), carry, e.outsideSignedRange(signedSum), nil
	case "/":
		if b.Sign() == 0 {
			return nil, false, false, &Error{Code: CodeDivisionByZero}
		}
		if e.opts.Signed {
			return new(big.Int).And(new(big.Int).Quo(e.signed(a), e.signed(b)), e.mask), false, false, nil
		}
		return new(big.Int).And(new(big.Int).Div(a, b), e.mask), false, false, nil
	case "%":
		if b.Sign() == 0 {
			return nil, false, false, &Error{Code: CodeDivisionByZero}
		}
		if e.opts.Signed {
			// The remainder has the sign of the dividend, like the scientific mod (D14).
			return new(big.Int).And(new(big.Int).Rem(e.signed(a), e.signed(b)), e.mask), false, false, nil
		}
		return new(big.Int).And(new(big.Int).Mod(a, b), e.mask), false, false, nil
	case "<<", ">>":
		count, err := e.shiftCount(b)
		if err != nil {
			return nil, false, false, err
		}
		countU := uint(count.Int64())
		shifted := new(big.Int)
		if operator == "<<" {
			shifted.Lsh(a, countU)
		} else if e.opts.Signed {
			// Arithmetic shift of the two's complement reading.
			shifted.Rsh(e.signed(a), countU)
		} else {
			shifted.Rsh(a, countU)
		}
		return new(big.Int).And(shifted, e.mask), false, false, nil
	case "&":
		return new(big.Int).And(a, b), false, false, nil
	case "|":
		return new(big.Int).Or(a, b), false, false, nil
	case "^":
		return new(big.Int).Xor(a, b), false, false, nil
	}
	return nil, false, false, &Error{Code: CodeSyntax}
}

// signed reads a masked value as a two's complement integer.
func (e *intEval) signed(value *big.Int) *big.Int {
	signed := new(big.Int).And(value, e.mask)
	if signed.Bit(e.word-1) == 1 {
		signed.Sub(signed, new(big.Int).Lsh(big.NewInt(1), uint(e.word)))
	}
	return signed
}

func (e *intEval) outsideSignedRange(value *big.Int) bool {
	return value.Cmp(e.rangeTop) > 0 || value.Cmp(e.rangeBottom) < 0
}
