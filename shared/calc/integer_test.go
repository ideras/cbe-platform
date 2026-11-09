package calc

import (
	"math/big"
	"math/rand"
	"strings"
	"testing"
)

func allowedProgrammer() map[string]bool {
	return Allowed(Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeProgrammer}}}.Effective())
}

// intEval is the tests' entry: an error fails the test unless wantCode names the expected failure.
func evalInt(t *testing.T, input string, opts IntOptions) (IntResult, string) {
	t.Helper()
	result, err := EvaluateInt(input, opts)
	if err == nil {
		return result, ""
	}
	code, ok := err.(*Error)
	if !ok {
		t.Fatalf("%q: unexpected error %v", input, err)
	}
	return result, code.Code
}

func intOK(t *testing.T, input string, opts IntOptions) IntResult {
	t.Helper()
	result, code := evalInt(t, input, opts)
	if code != "" {
		t.Fatalf("%q: unexpected %s", input, code)
	}
	return result
}

func optsInt(word int, signed bool) IntOptions {
	return IntOptions{Word: word, Signed: signed, Allowed: allowedProgrammer()}
}

// TestIntegerArithmeticAndFlags covers the plan's minimum vectors (Phase 5), with the D15 amendment: the
// signed/unsigned toggle decides how /, % and >> read operands, and >>> does not exist.
func TestIntegerArithmeticAndFlags(t *testing.T) {
	for name, test := range map[string]struct {
		input           string
		opts            IntOptions
		bits            uint64
		carry, overflow bool
	}{
		"0xFF + 1 at 8 bits":          {"0xFF + 1", optsInt(8, true), 0, true, false},
		"127 + 1 at 8 bits":           {"127 + 1", optsInt(8, true), 0x80, false, true},
		"-1 at 8 bits":                {"-1", optsInt(8, true), 0xFF, false, false},
		"0 - 1 at 8 bits":             {"0 - 1", optsInt(8, true), 0xFF, true, false},
		"0xFF + 1 - 1 at 8 bits":      {"0xFF + 1 - 1", optsInt(8, true), 0xFF, true, false},
		"0xFF / 2 signed at 8 bits":   {"0xFF / 2", optsInt(8, true), 0, false, false},
		"0xFF / 2 unsigned at 8 bits": {"0xFF / 2", optsInt(8, false), 127, false, false},
		"1 << 31 at 32 bits":          {"1 << 31", optsInt(32, true), 0x80000000, false, false},
		"-8 >> 1 signed at 8 bits":    {"-8 >> 1", optsInt(8, true), 0xFC, false, false},
		"0x80 >> 1 unsigned":          {"0x80 >> 1", optsInt(8, false), 0x40, false, false},
		"rol(0x80, 1) at 8 bits":      {"rol(0x80, 1)", optsInt(8, true), 0x01, false, false},
		"7 / -2 at 8 bits":            {"7 / -2", optsInt(8, true), 0xFD, false, false},
		"-7 % 3 at 8 bits":            {"-7 % 3", optsInt(8, true), 0xFF, false, false},
		"64-bit max":                  {"0xFFFFFFFFFFFFFFFF", optsInt(64, true), 0xFFFFFFFFFFFFFFFF, false, false},
		"1 + 2 << 3 at 8 bits":        {"1 + 2 << 3", optsInt(8, true), 24, false, false},
		"6 & 3 | 8 at 8 bits":         {"6 & 3 | 8", optsInt(8, true), 10, false, false},
		"1 | 2 ^ 3 at 8 bits":         {"1 | 2 ^ 3", optsInt(8, true), 1, false, false},
		"0xff + 1 at 16 bits":         {"0xff + 1", optsInt(16, true), 256, false, false},
		"1 << 4 is decimal":           {"1 << 4", optsInt(32, true), 16, false, false},
		"every prefix, either case":   {"0b101 + 0B1 + 0o17 + 0O1 + 0xA + 0Xa + 0d9 + 0D1", optsInt(32, true), 5 + 1 + 15 + 1 + 10 + 10 + 9 + 1, false, false},
		"zero alone":                  {"0", optsInt(8, true), 0, false, false},
		"0d may start with 0":         {"0d010", optsInt(8, true), 10, false, false},
		"separators group digits":     {"0b1010_0101 + 1_000", optsInt(16, true), 0xA5 + 1000, false, false},
		"separators in hex and octal": {"0xFF_FF - 0o7_7", optsInt(32, true), 0xFFFF - 077, false, false},
		"nand and nor":                {"nand(0x0F, 0x3C)", optsInt(8, true), 0xF3, false, false},
		"ror":                         {"ror(0x01, 1)", optsInt(8, true), 0x80, false, false},
		"unary ~":                     {"~0x00", optsInt(8, true), 0xFF, false, false},
		"unsigned * with carry":       {"16 * 16", optsInt(8, true), 0, true, true},
		"signed * with overflow":      {"64 * 2", optsInt(8, true), 0x80, false, true},
		"shift of a shift":            {"1 << 2 << 3", optsInt(8, true), 32, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			result := intOK(t, test.input, test.opts)
			if result.Bits != test.bits || result.Carry != test.carry || result.Overflow != test.overflow {
				t.Fatalf("%q = bits %d (%X) CF=%v OF=%v, want bits %d (%X) CF=%v OF=%v", test.input, result.Bits, result.Bits, result.Carry, result.Overflow, test.bits, test.bits, test.carry, test.overflow)
			}
		})
	}
	// The readings of a few results, in the words the panel shows.
	if got := FormatInt(0xFF, 8); got.Signed != "-1" || got.Unsigned != "255" || got.Hex != "0xFF" || got.Octal != "377" || got.Binary != "11111111" {
		t.Fatalf("FormatInt(0xFF, 8) = %+v", got)
	}
	if got := FormatInt(0xFFFFFFFFFFFFFFFF, 64); got.Signed != "-1" || got.Unsigned != "18446744073709551615" || got.Hex != "0xFFFFFFFFFFFFFFFF" {
		t.Fatalf("FormatInt(64-bit max) = %+v", got)
	}
	if got := FormatInt(0x80000000, 32); got.Signed != "-2147483648" || got.Unsigned != "2147483648" {
		t.Fatalf("FormatInt(1 << 31) = %+v", got)
	}
}

func TestIntegerRefusals(t *testing.T) {
	for name, test := range map[string]struct {
		input string
		opts  IntOptions
		code  string
	}{
		"literal does not fit":        {"256", optsInt(8, true), CodeOverflow},
		"hex literal does not fit":    {"0x1FF", optsInt(8, true), CodeOverflow},
		"shift count at the word":     {"1 << 8", optsInt(8, true), CodeShiftRange},
		"negative shift count":        {"1 << -1", optsInt(8, true), CodeShiftRange},
		"rotate count at the word":    {"rol(1, 8)", optsInt(8, true), CodeShiftRange},
		"negative rotate count":       {"ror(1, -1)", optsInt(8, true), CodeShiftRange},
		"division by zero":            {"1 / 0", optsInt(8, true), CodeDivisionByZero},
		"remainder by zero":           {"1 % 0", optsInt(8, true), CodeDivisionByZero},
		"unknown function":            {"foo(1)", optsInt(8, true), CodeUnknownFunction},
		"disallowed function":         {"nand(1, 2)", IntOptions{Word: 8, Allowed: map[string]bool{}}, CodeUnknownFunction},
		"bare name in DEC base":       {"ff", optsInt(8, true), CodeUnknownIdent},
		"bare name that is no ident":  {"zzz", optsInt(8, true), CodeUnknownIdent},
		"a prefix with no digits":     {"0x", optsInt(8, true), CodeBadNumber},
		"binary digits out of base":   {"0b12", optsInt(8, true), CodeBadNumber},
		"octal digits out of base":    {"0o8", optsInt(8, true), CodeBadNumber},
		"hex without its prefix":      {"1F", optsInt(8, true), CodeBadNumber},
		"a binary suffix":             {"1010b", optsInt(8, true), CodeBadNumber},
		"a leading zero":              {"010", optsInt(8, true), CodeBadNumber},
		"only zeros":                  {"00", optsInt(8, true), CodeBadNumber},
		"a separator after the zero":  {"0_1", optsInt(8, true), CodeBadNumber},
		"a separator after a prefix":  {"0x_FF", optsInt(8, true), CodeBadNumber},
		"a trailing separator":        {"1_", optsInt(8, true), CodeBadNumber},
		"a doubled separator":         {"1__0", optsInt(8, true), CodeBadNumber},
		"number after number":         {"0x10 0x20", optsInt(8, true), CodeSyntax},
		"trailing operator":           {"1 +", optsInt(8, true), CodeSyntax},
		"double factorial is garbage": {"5!", optsInt(8, true), CodeSyntax},
		"a real is no integer":        {"1.5", optsInt(8, true), CodeNotInteger},
		"too long":                    {strings.Repeat("1+", 100) + "1", optsInt(8, true), CodeTooLong},
		"too deep":                    {strings.Repeat("(", 51) + "1" + strings.Repeat(")", 51), optsInt(8, true), CodeTooDeep},
	} {
		t.Run(name, func(t *testing.T) {
			if _, code := evalInt(t, test.input, test.opts); code != test.code {
				t.Fatalf("%q = %s, want %s", test.input, code, test.code)
			}
		})
	}
}

// The ans of the programmer mode flows from one evaluation to the next (D10).
func TestIntegerAns(t *testing.T) {
	opts := optsInt(8, true)
	opts.Ans = Value{Bits: 41}
	if got := intOK(t, "ans + 1", opts); got.Bits != 42 {
		t.Fatalf("ans + 1 = %d", got.Bits)
	}
	// `ans` cannot be disabled and is not a function name.
	opts = optsInt(8, true)
	if _, code := evalInt(t, "ans", opts); code != "" {
		t.Fatalf("bare ans = %s", code)
	}
}

// Property test (plan Phase 5): for random 64-bit a and b and each op in {+, -, *}, the result equals
// (a op b) mod 2^64 computed with big.Int, and the flags match a big.Int reference.
func TestIntegerArithmeticMatchesTheBigReference(t *testing.T) {
	generator := rand.New(rand.NewSource(2035))
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	top := new(big.Int).Lsh(big.NewInt(1), 64)
	signedBottom := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 63))
	signedTop := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 63), big.NewInt(1))
	signed := func(v *big.Int) *big.Int {
		masked := new(big.Int).And(v, mask)
		if masked.Bit(63) == 1 {
			masked.Sub(masked, top)
		}
		return masked
	}
	for i := 0; i < 10000; i++ {
		a := generator.Uint64()
		b := generator.Uint64()
		for _, op := range []string{"+", "-", "*"} {
			result, err := EvaluateInt(uint64HexOrDec(a)+" "+op+" "+uint64HexOrDec(b), IntOptions{Word: 64, Signed: true, Allowed: allowedProgrammer()})
			if err != nil {
				t.Fatalf("%d %s %d: %v", a, op, b, err)
			}
			bigA, bigB := new(big.Int).SetUint64(a), new(big.Int).SetUint64(b)
			want := new(big.Int)
			switch op {
			case "+":
				want.Add(bigA, bigB)
			case "-":
				want.Sub(bigA, bigB)
			case "*":
				want.Mul(bigA, bigB)
			}
			if got := new(big.Int).SetUint64(result.Bits); got.Cmp(new(big.Int).And(want, mask)) != 0 {
				t.Fatalf("%d %s %d = %d, want %d", a, op, b, got, new(big.Int).And(want, mask))
			}
			wantCarry := false
			switch op {
			case "+":
				wantCarry = want.Cmp(top) >= 0
			case "-":
				wantCarry = bigA.Cmp(bigB) < 0
			case "*":
				wantCarry = want.Cmp(top) >= 0
			}
			if result.Carry != wantCarry {
				t.Fatalf("%d %s %d: CF=%v, want %v", a, op, b, result.Carry, wantCarry)
			}
			signedWant := new(big.Int)
			switch op {
			case "+":
				signedWant.Add(signed(bigA), signed(bigB))
			case "-":
				signedWant.Sub(signed(bigA), signed(bigB))
			case "*":
				signedWant.Mul(signed(bigA), signed(bigB))
			}
			wantOverflow := signedWant.Cmp(signedTop) > 0 || signedWant.Cmp(signedBottom) < 0
			if result.Overflow != wantOverflow {
				t.Fatalf("%d %s %d: OF=%v, want %v", a, op, b, result.Overflow, wantOverflow)
			}
		}
	}
}

func uint64HexOrDec(value uint64) string {
	if value < 1000000 {
		return bigFromUint64(value).String()
	}
	return "0d" + bigFromUint64(value).String()
}

func bigFromUint64(value uint64) *big.Int { return new(big.Int).SetUint64(value) }
