package calc

import (
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The programmer evaluator: a separate entry point and grammar from the scientific one (plan Phase 5), with
// integer arithmetic at a word size, the bitwise operators, and the CPU flags of the last operator evaluated.
// There is no `>>>` (D15, owner 2026-10-21): `>>` follows the signed/unsigned toggle, arithmetic when signed and
// logical when unsigned. `^` is XOR here and power in the scientific mode.
//
// Flags never change with the toggle (they are two readings of the same computation, as on real hardware): CF
// and OF of an addition are what they are, whatever the operands are taken to mean afterwards.

// IntOptions are the panel's settings: the word size (8, 16, 32 or 64), the signed/unsigned toggle (D15), the
// input base for unprefixed literals, the allowed names, and the programmer ans (D10) with its kind.
type IntOptions struct {
	Word    int
	Signed  bool
	Base    int // 2, 8, 10 or 16; anything else reads as 10
	Allowed map[string]bool
	Ans     Value
}

// Value is a programmer result (D20): an integer, the N-bit value with the flags of the last operator evaluated,
// the root of the expression tree; or a real, an exact rational, which sets no flags. The readings of an integer
// (signed and unsigned decimal, hex, octal, binary) come from FormatInt, a real's from FormatReal.
type Value struct {
	Real         *big.Rat // non-nil for a real
	NegativeZero bool     // a real zero is -0 (IEEE 754's signed zero)
	Bits         uint64
	Carry        bool
	Overflow     bool
}

// IsReal says the value is a real, not an integer.
func (v Value) IsReal() bool { return v.Real != nil }

// IntResult is an integer outcome, what EvaluateInt answers.
type IntResult struct {
	Bits     uint64
	Carry    bool
	Overflow bool
}

// IntDisplay is one result read at its word size, in every base the panel shows.
type IntDisplay struct {
	Signed, Unsigned, Hex, Octal, Binary string
}

// EvaluateProgrammer evaluates one programmer expression: integer literals in the input base or with an explicit
// prefix, point literals (D19), the operators, `ans`, and the allowed registry functions of programmer mode
// (nand, nor, rol, ror). A failed evaluation is a *Error with a Code; the checks of a call run allowed → arity →
// evaluation, as everywhere.
func EvaluateProgrammer(input string, opts IntOptions) (Value, error) {
	return evaluateProgrammer(input, opts, CodeOverflow)
}

// EvaluateInt is EvaluateProgrammer for an integer result: a real one is NOT_INTEGER.
func EvaluateInt(input string, opts IntOptions) (IntResult, error) {
	value, err := EvaluateProgrammer(input, opts)
	if err != nil {
		return IntResult{}, err
	}
	if value.IsReal() {
		return IntResult{}, &Error{Code: CodeNotInteger}
	}
	return IntResult{Bits: value.Bits, Carry: value.Carry, Overflow: value.Overflow}, nil
}

// evaluateProgrammer is the evaluator, with the code an integer literal too wide for the word answers: OVERFLOW,
// or BIT_WIDTH when the inspector decodes at the format's width.
func evaluateProgrammer(input string, opts IntOptions, fitCode string) (Value, error) {
	if utf8.RuneCountInString(input) > maxInputLength {
		return Value{}, &Error{Code: CodeTooLong}
	}
	word := effectiveWord(opts.Word)
	if opts.Base != 2 && opts.Base != 8 && opts.Base != 16 {
		opts.Base = 10
	}
	tokens, lexErr := lexInt(input, opts.Base)
	if lexErr != nil {
		return Value{}, lexErr
	}
	if len(tokens) == 0 {
		return Value{}, &Error{Code: CodeSyntax}
	}
	ev := &intEval{opts: opts, word: word, fitCode: fitCode}
	ev.mask = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(word)), big.NewInt(1))
	ev.rangeTop = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(word-1)), big.NewInt(1))
	ev.rangeBottom = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(word-1)))
	root, parseErr := ev.parse(tokens)
	if parseErr != nil {
		return Value{}, parseErr
	}
	value, carry, overflow, evalErr := ev.eval(root)
	if evalErr != nil {
		return Value{}, evalErr
	}
	if value.isReal() {
		return Value{Real: value.r, NegativeZero: value.r.Sign() == 0 && value.negative}, nil
	}
	if !value.i.IsUint64() {
		return Value{}, &Error{Code: CodeOverflow} // unreachable after masking; a guard for the refactor
	}
	return Value{Bits: value.i.Uint64(), Carry: carry, Overflow: overflow}, nil
}

// effectiveWord is the word size an evaluation uses: the panel's, or the default 32 (D5) for anything the panel
// never sends.
func effectiveWord(word int) int {
	if word != 8 && word != 16 && word != 32 && word != 64 {
		return 32
	}
	return word
}

// Rational is the value as a number: a real as it is, an integer through the signed/unsigned toggle (the owner's
// answer of 2026-10-24), so 8-bit 0xFF is -1 when signed and 255 when unsigned. The Radix Point and IEEE 754 tabs
// read it.
func (v Value) Rational(word int, signed bool) *big.Rat {
	if v.IsReal() {
		return new(big.Rat).Set(v.Real)
	}
	if signed {
		return new(big.Rat).SetInt64(signExtend(v.Bits, word))
	}
	return new(big.Rat).SetInt(new(big.Int).SetUint64(v.Bits))
}

// FormatReal is a real's display, by the scientific mode's rule (Format): 12 significant digits, fixed for
// 1e-6 ≤ |v| < 1e12 and scientific otherwise. The value keeps its exactness; only the display rounds.
func FormatReal(r *big.Rat, negativeZero bool) string {
	if r.Sign() == 0 {
		if negativeZero {
			return "-0"
		}
		return "0"
	}
	value := new(big.Float).SetPrec(256).SetRat(r)
	mantissa, exponentText, _ := strings.Cut(value.Text('e', 11), "e")
	exponent, _ := strconv.Atoi(exponentText)
	if exponent >= -6 && exponent < 12 {
		return trimZeros(value.Text('f', 11-exponent))
	}
	sign := "+"
	if exponent < 0 {
		sign, exponent = "-", -exponent
	}
	return trimZeros(mantissa) + "e" + sign + strconv.Itoa(exponent)
}

// FormatInt reads an N-bit value in every base the panel shows, flags aside.
func FormatInt(bits uint64, word int) IntDisplay {
	unsigned := strconv.FormatUint(bits, 10)
	signed := strconv.FormatInt(signExtend(bits, word), 10)
	digits := word / 4
	hex := "0x" + strings.ToUpper(strconv.FormatUint(bits, 16))
	if len(hex)-2 < digits {
		hex = "0x" + strings.Repeat("0", digits-len(hex)+2) + hex[2:]
	}
	octDigits := (word + 2) / 3
	octal := strings.Repeat("0", octDigits-len(strconv.FormatUint(bits, 8))) + strconv.FormatUint(bits, 8)
	binary := strings.Repeat("0", word-len(strconv.FormatUint(bits, 2))) + strconv.FormatUint(bits, 2)
	return IntDisplay{Signed: signed, Unsigned: unsigned, Hex: hex, Octal: octal, Binary: binary}
}

// signExtend reads an N-bit pattern as a two's complement integer.
func signExtend(bits uint64, word int) int64 {
	if word >= 64 {
		return int64(bits)
	}
	return int64(bits<<(64-uint(word))) >> (64 - uint(word))
}
