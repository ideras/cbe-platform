package calc

import (
	"math/big"
	"testing"
)

// The test vectors of Phase 6A: point literals (D19), integers and reals (D20), and the shared input read by
// the Radix Point and IEEE 754 tabs (D21).

func evalProgrammer(t *testing.T, input string, opts IntOptions) (Value, string) {
	t.Helper()
	value, err := EvaluateProgrammer(input, opts)
	if err == nil {
		return value, ""
	}
	code, ok := err.(*Error)
	if !ok {
		t.Fatalf("%q: unexpected error %v", input, err)
	}
	return value, code.Code
}

func rat(text string) *big.Rat {
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		panic(text)
	}
	return value
}

func TestProgrammerRealValues(t *testing.T) {
	for name, test := range map[string]struct {
		input string
		opts  IntOptions
		want  string // the exact rational, as big.Rat writes it
	}{
		"exact decimal":                   {"10.625", optsInt(32, true, 10), "85/8"},
		"a whole real stays real":         {"2.0 + 1", optsInt(32, true, 10), "3"},
		"a real ignores the word size":    {"300.0", optsInt(8, true, 10), "300"},
		"real addition":                   {"1.5 + 1", optsInt(32, true, 10), "5/2"},
		"real division is exact":          {"1.0 / 3", optsInt(32, true, 10), "1/3"},
		"unary minus of a real":           {"-0.5", optsInt(32, true, 10), "-1/2"},
		"0d takes a point in HEX":         {"0d10.5", optsInt(32, true, 16), "21/2"},
		"0d takes a point in BIN":         {"0d0.25", optsInt(32, true, 2), "1/4"},
		"8-bit 0xFF is -1 when signed":    {"0xFF + 0.5", optsInt(8, true, 10), "-1/2"},
		"8-bit 0xFF is 255 when unsigned": {"0xFF + 0.5", optsInt(8, false, 10), "511/2"},
		"a real times an integer":         {"0.5 * 3", optsInt(32, true, 10), "3/2"},
		"precedence with reals":           {"1 + 0.5 * 4", optsInt(32, true, 10), "3"},
	} {
		t.Run(name, func(t *testing.T) {
			value, code := evalProgrammer(t, test.input, test.opts)
			if code != "" {
				t.Fatalf("%q: %s", test.input, code)
			}
			if !value.IsReal() || value.Real.Cmp(rat(test.want)) != 0 {
				t.Fatalf("%q = %+v, want the real %s", test.input, value, test.want)
			}
			if value.Carry || value.Overflow {
				t.Fatalf("%q: a real sets no flags: %+v", test.input, value)
			}
		})
	}
}

func TestProgrammerIntegersStayIntegers(t *testing.T) {
	for name, test := range map[string]struct {
		input string
		opts  IntOptions
		bits  uint64
	}{
		"integer division truncates":     {"1 / 3", optsInt(32, true, 10), 0},
		"0x1e3 is hex in DEC":            {"0x1e3", optsInt(32, true, 10), 483},
		"0x1e3 is hex in HEX":            {"0x1e3", optsInt(32, true, 16), 483},
		"bare 1e3 is hex in HEX":         {"1e3", optsInt(32, true, 16), 483},
		"0x1e3f is one hex integer":      {"0x1e3f", optsInt(32, true, 10), 0x1E3F},
		"signed 8-bit 0xFF / 2 is -1/2":  {"0xFF / 2", optsInt(8, true, 10), 0},
		"unsigned 8-bit 0xFF / 2 is 127": {"0xFF / 2", optsInt(8, false, 10), 127},
	} {
		t.Run(name, func(t *testing.T) {
			value, code := evalProgrammer(t, test.input, test.opts)
			if code != "" || value.IsReal() || value.Bits != test.bits {
				t.Fatalf("%q = %+v %s, want the integer %d", test.input, value, code, test.bits)
			}
		})
	}
}

func TestProgrammerRealRefusals(t *testing.T) {
	for name, test := range map[string]struct {
		input string
		opts  IntOptions
		code  string
	}{
		"no leading digit":            {".5", optsInt(32, true, 10), CodeSyntax},
		"a point in a hex literal":    {"0x1.8", optsInt(32, true, 10), CodeSyntax},
		"a point in a binary literal": {"0b1.1", optsInt(32, true, 10), CodeSyntax},
		"a point in an octal literal": {"0o1.1", optsInt(32, true, 10), CodeSyntax},
		"no exponent in DEC":          {"1e3", optsInt(32, true, 10), CodeSyntax},
		"no exponent after a point":   {"1.5e3", optsInt(32, true, 10), CodeSyntax},
		"a point in HEX":              {"10.5", optsInt(32, true, 16), CodeSyntax},
		"a point in BIN":              {"1.1", optsInt(32, true, 2), CodeSyntax},
		"a hex word with a point":     {"ff.8", optsInt(32, true, 16), CodeSyntax},
		"no digits after the point":   {"1.", optsInt(32, true, 10), CodeSyntax},
		"two points":                  {"1.2.3", optsInt(32, true, 10), CodeSyntax},
		"real AND":                    {"1.5 & 1", optsInt(32, true, 10), CodeNotInteger},
		"a whole real is still real":  {"2.0 & 1", optsInt(32, true, 10), CodeNotInteger},
		"real remainder":              {"5.0 % 2", optsInt(32, true, 10), CodeNotInteger},
		"real NOT":                    {"~1.5", optsInt(32, true, 10), CodeNotInteger},
		"real shift count":            {"1 << 0.5", optsInt(32, true, 10), CodeNotInteger},
		"whole real shift count":      {"1 << 2.0", optsInt(32, true, 10), CodeNotInteger},
		"real shifted":                {"2.0 >> 1", optsInt(32, true, 10), CodeNotInteger},
		"real XOR":                    {"1 ^ 0.5", optsInt(32, true, 10), CodeNotInteger},
		"real OR":                     {"1 | 0.5", optsInt(32, true, 10), CodeNotInteger},
		"real rotate count":           {"rol(1, 2.0)", optsInt(32, true, 10), CodeNotInteger},
		"real nand":                   {"nand(1.0, 1)", optsInt(32, true, 10), CodeNotInteger},
		"real division by zero":       {"1.0 / 0", optsInt(32, true, 10), CodeDivisionByZero},
		"a disallowed call first":     {"foo(1.5)", optsInt(32, true, 10), CodeUnknownFunction},
		"an integer literal still":    {"256 + 0.5", optsInt(8, true, 10), CodeOverflow},
	} {
		t.Run(name, func(t *testing.T) {
			if _, code := evalProgrammer(t, test.input, test.opts); code != test.code {
				t.Fatalf("%q = %q, want %s", test.input, code, test.code)
			}
		})
	}
}

// A real cannot grow without bound: ans * ans repeated stops at OVERFLOW instead of eating the memory.
func TestProgrammerRealSizeIsBounded(t *testing.T) {
	opts := optsInt(32, true, 10)
	opts.Ans = Value{Real: rat("1.0000001")}
	for i := 0; i < 20; i++ {
		value, code := evalProgrammer(t, "ans * ans", opts)
		if code == CodeOverflow {
			return
		}
		if code != "" {
			t.Fatalf("round %d: %s", i, code)
		}
		opts.Ans = value
	}
	t.Fatal("ans * ans never reached the size bound")
}

// The programmer ans keeps its kind: a real stays a real, an integer is read at the current word size.
func TestProgrammerAnsKeepsItsKind(t *testing.T) {
	opts := optsInt(32, true, 10)
	opts.Ans = Value{Real: rat("1.5")}
	if value, code := evalProgrammer(t, "ans + 1", opts); code != "" || !value.IsReal() || value.Real.Cmp(rat("2.5")) != 0 {
		t.Fatalf("real ans + 1 = %+v %s", value, code)
	}
	if _, code := evalProgrammer(t, "ans & 1", opts); code != CodeNotInteger {
		t.Fatalf("real ans & 1 = %s, want NOT_INTEGER", code)
	}
	opts = optsInt(8, true, 10)
	opts.Ans = Value{Bits: 0x1FF}
	if value, code := evalProgrammer(t, "ans", opts); code != "" || value.Bits != 0xFF {
		t.Fatalf("a 9-bit ans at 8 bits = %+v %s", value, code)
	}
}

// IEEE 754's signed zero: -0.0 is negative, and arithmetic gives zero the IEEE sign.
func TestProgrammerNegativeZero(t *testing.T) {
	for input, negative := range map[string]bool{
		"-0.0": true, "0.0": false, "-(-0.0)": false, "-0": false, // -0 is the integer 0
		"-0.0 * 1": true, "0.0 * -1": true, "-0.0 / -1": false, "-0.0 + -0.0": true, "1.0 - 1": false, "-0.0 - 0.0": true,
	} {
		value, code := evalProgrammer(t, input, optsInt(32, true, 10))
		if code != "" {
			t.Fatalf("%q: %s", input, code)
		}
		if value.NegativeZero != negative {
			t.Fatalf("%q negative zero = %v, want %v", input, value.NegativeZero, negative)
		}
	}
}

func TestFormatReal(t *testing.T) {
	for text, want := range map[string]string{
		"10.625": "10.625", "1/3": "0.333333333333", "-1/2": "-0.5", "3": "3", "1000000000000": "1e+12",
		"1/10000000": "1e-7", "0.000001": "0.000001", "123456789012345": "1.23456789012e+14",
		"999999999999.9999": "1e+12",
	} {
		if got := FormatReal(rat(text), false); got != want {
			t.Fatalf("FormatReal(%s) = %q, want %q", text, got, want)
		}
	}
	if got := FormatReal(new(big.Rat), true); got != "-0" {
		t.Fatalf("FormatReal(-0) = %q", got)
	}
}
