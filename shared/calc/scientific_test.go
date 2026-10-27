package calc

import (
	"math"
	"strings"
	"testing"
)

// allowedScientific is the set Allowed gives for a scientific-only calculator with nothing disabled.
func allowedScientific() map[string]bool {
	return Allowed(Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific}}}.Effective())
}

// evaluate is the one entry the tests use: an error fails the test with its code, unless wantCode says the
// failure is the point.
func evaluate(t *testing.T, input, angleMode string, opts Options) (Result, *Error) {
	t.Helper()
	result, err := Evaluate(input, opts)
	if err == nil {
		return result, nil
	}
	code, ok := err.(*Error)
	if !ok {
		t.Fatalf("%q: unexpected error %v", input, err)
	}
	return result, code
}

func evalOK(t *testing.T, input, angleMode string, ans float64) Result {
	t.Helper()
	opts := Options{AngleMode: angleMode, Allowed: allowedScientific(), Ans: num(ans)}
	result, code := evaluate(t, input, angleMode, opts)
	if code != nil {
		t.Fatalf("%q: unexpected %s", input, code.Code)
	}
	return result
}

func evalCode(t *testing.T, input, angleMode string) string {
	t.Helper()
	opts := Options{AngleMode: angleMode, Allowed: allowedScientific()}
	_, code := evaluate(t, input, angleMode, opts)
	if code == nil {
		t.Fatalf("%q: expected an error, got %+v", input, Options{})
	}
	return code.Code
}

// TestArithmeticAndPrecedence covers the worked examples of the plan.
func TestArithmeticAndPrecedence(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"2+3*4", "14"},
		{"(1+2)*3", "9"},
		{"-2^2", "-4"},
		{"2^3^2", "512"},
		{"2^-1", "0.5"},
		{"-3!", "-6"},
		{"2 - 3", "-1"},
		{"2 -3", "-1"},
		{"+5", "5"},
	} {
		if got := evalOK(t, test.input, "rad", 0).Display; got != test.display {
			t.Errorf("%q = %q, want %q", test.input, got, test.display)
		}
	}
}

func TestImplicitMultiplication(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"2pi", "6.28318530718"},
		{"3(4+1)", "15"},
		{"1/2pi", "1.57079632679"},
		{"2e", "5.43656365692"},
		{"2e3", "2000"},
		{"2e+1", "20"},
		{"pi 2", ""},
		{"2 3", ""},
		{"2 3+1", ""},
	} {
		if test.display == "" {
			// A number directly followed by a number is SYNTAX; a value followed by a number ends the expression.
			if code := evalCode(t, test.input, "rad"); code != CodeSyntax {
				t.Errorf("%q = %s, want SYNTAX", test.input, code)
			}
			continue
		}
		if got := evalOK(t, test.input, "rad", 0).Display; got != test.display {
			t.Errorf("%q = %q, want %q", test.input, got, test.display)
		}
	}
}

func TestLogarithmsAndPowers(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"log10(1000)", "3"},
		{"log(8, 2)", "3"},
		{"ln(e)", "1"},
		{"cbrt(-8)", "-2"},
		{"exp(0)", "1"},
		{"abs(-3)", "3"},
		{"floor(2.7)", "2"},
		{"ceil(2.1)", "3"},
		{"(-8)^(1/3)", ""},
		{"(-2)^3", "-8"},
	} {
		if test.display == "" {
			if code := evalCode(t, test.input, "rad"); code != CodeDomain {
				t.Errorf("%q = %s, want DOMAIN", test.input, code)
			}
			continue
		}
		if got := evalOK(t, test.input, "rad", 0).Display; got != test.display {
			t.Errorf("%q = %q, want %q", test.input, got, test.display)
		}
	}
	if code := evalCode(t, "log(8)", "rad"); code != CodeArity {
		t.Errorf("log(8) = %s, want ARITY (D2)", code)
	}
	if code := evalCode(t, "log(8, 2, 3)", "rad"); code != CodeArity {
		t.Errorf("log(8, 2, 3) = %s, want ARITY", code)
	}
}

func TestDegreeMode(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"sin(30)", "0.5"},
		{"sin(180)", "0"},
		{"cos(90)", "0"},
		{"sin(540)", "0"},  // reduced modulo 360 first
		{"sin(-90)", "-1"}, // exact multiple of 90, negative side
		{"cos(0)", "1"},
		{"tan(0)", "0"},
		{"tan(180)", "0"},
		{"atan2(1,1)", "45"},
		{"asin(0.5)", "30"},
		{"acos(0.5)", "60"},
		{"atan(1)", "45"},
		{"2sin(30)", "1"}, // implicit multiplication by a call
	} {
		if got := evalOK(t, test.input, "deg", 0).Display; got != test.display {
			t.Errorf("%q = %q, want %q", test.input, got, test.display)
		}
	}
	if code := evalCode(t, "tan(90)", "deg"); code != CodeDomain {
		t.Errorf("tan(90) = %s, want DOMAIN", code)
	}
	if code := evalCode(t, "tan(270)", "deg"); code != CodeDomain {
		t.Errorf("tan(270) = %s, want DOMAIN", code)
	}
	// rad keeps its float noise: no correction is made (Appendix B).
	got := evalOK(t, "sin(pi)", "rad", 0).Value.Number()
	if math.Abs(got-1.2246467991473532e-16) > 1e-30 {
		t.Errorf("sin(pi) in rad = %g", got)
	}
	if display := evalOK(t, "sin(pi/6)", "rad", 0).Display; display != "0.5" {
		t.Errorf("sin(pi/6) in rad = %q, want 0.5", display)
	}
}

func TestFactorial(t *testing.T) {
	if got := evalOK(t, "5!", "rad", 0).Display; got != "120" {
		t.Errorf("5! = %q", got)
	}
	for _, test := range []struct{ input, code string }{
		{"(-1)!", CodeDomain},
		{"2.5!", CodeDomain},
		{"171!", CodeOverflow},
		{"5!!", CodeSyntax},
	} {
		if code := evalCode(t, test.input, "rad"); code != test.code {
			t.Errorf("%q = %s, want %s", test.input, code, test.code)
		}
	}
}

func TestDomainAndDivisionFailures(t *testing.T) {
	for _, test := range []struct{ input, code string }{
		{"1/0", CodeDivisionByZero},
		{"mod(1, 0)", CodeDivisionByZero},
		{"log10(0)", CodeDomain},
		{"log10(-1)", CodeDomain},
		{"ln(0)", CodeDomain},
		{"log(0, 2)", CodeDomain},
		{"log(8, 0)", CodeDomain},
		{"log(8, 1)", CodeDomain},
		{"sqrt(-1)", CodeDomain},
		{"asin(2)", CodeDomain},
		{"acos(-1.5)", CodeDomain},
		{"mod(-7, 3)", ""},
	} {
		if test.code == "" {
			if got := evalOK(t, test.input, "rad", 0).Display; got != "-1" {
				t.Errorf("%q = %q, want -1 (sign of the dividend, D14)", test.input, got)
			}
			continue
		}
		if code := evalCode(t, test.input, "rad"); code != test.code {
			t.Errorf("%q = %s, want %s", test.input, code, test.code)
		}
	}
	if got := evalOK(t, "round(2.5)", "rad", 0).Display; got != "3" {
		t.Errorf("round(2.5) = %q, want 3 (D14)", got)
	}
	if got := evalOK(t, "round(-2.5)", "rad", 0).Display; got != "-3" {
		t.Errorf("round(-2.5) = %q, want -3 (D14)", got)
	}
}

// The same error for unavailable and unknown: a disallowed function never answers ARITY, and a bare name is
// UNKNOWN_IDENT whether or not a function of that name exists.
func TestUnknownNamesLookAlike(t *testing.T) {
	// sinh not in Allowed: the same set minus that one name.
	withoutSinh := allowedScientific()
	delete(withoutSinh, "sinh")
	opts := Options{AngleMode: "rad", Allowed: withoutSinh}
	_, sinhErr := Evaluate("sinh(1)", opts)
	_, fooErr := Evaluate("foo(1)", opts)
	if sinhErr == nil || fooErr == nil || sinhErr.(*Error).Code != fooErr.(*Error).Code {
		t.Fatalf("sinh(1) and foo(1) must fail alike, got %v and %v", sinhErr, fooErr)
	}
	_, sinh2Err := Evaluate("sinh(1, 2)", opts)
	if sinh2Err == nil || sinh2Err.(*Error).Code != CodeUnknownFunction {
		t.Fatalf("sinh(1, 2) = %v, want UNKNOWN_FUNCTION, not ARITY", sinh2Err)
	}
	_, bareSinErr := Evaluate("sin", opts)
	_, bareFooErr := Evaluate("foo", opts)
	if bareSinErr == nil || bareFooErr == nil || bareSinErr.(*Error).Code != CodeUnknownIdent || bareFooErr.(*Error).Code != CodeUnknownIdent {
		t.Fatalf("bare `sin` and bare `foo` must both be UNKNOWN_IDENT, got %v and %v", bareSinErr, bareFooErr)
	}
	// A disallowed function never answers ARITY.
	_, logErr := Evaluate("log(8)", Options{Allowed: map[string]bool{}}) // even nothing is allowed
	if logErr == nil || logErr.(*Error).Code != CodeUnknownFunction {
		t.Fatalf("log(8) with nothing allowed = %v, want UNKNOWN_FUNCTION", logErr)
	}
	// `Sin` is not `sin`.
	if code := evalCode(t, "Sin(30)", "rad"); code != CodeUnknownFunction {
		t.Errorf("Sin(30) = %s, want UNKNOWN_FUNCTION", code)
	}
}

func TestAnsAndPi(t *testing.T) {
	if got := evalOK(t, "ans + 1", "rad", 41).Display; got != "42" {
		t.Errorf("ans + 1 = %q, want 42", got)
	}
	if got := evalOK(t, "ans", "rad", 2.5).Display; got != "2.5" {
		t.Errorf("ans = %q", got)
	}
	// pi disabled is an unknown identifier.
	opts := Options{AngleMode: "rad", Allowed: Allowed(Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific}, DisabledFunctions: []string{"pi"}}}.Effective())}
	_, err := Evaluate("pi", opts)
	if err == nil || err.(*Error).Code != CodeUnknownIdent {
		t.Fatalf("disabled pi = %v, want UNKNOWN_IDENT", err)
	}
}

func TestLimits(t *testing.T) {
	long := strings.Repeat("1+", 100) + "1" // 201 code points
	if code := evalCode(t, long, "rad"); code != CodeTooLong {
		t.Errorf("a 201-character input = %s, want TOO_LONG", code)
	}
	atLimit := strings.Repeat("1+", 99) + "1" // 199 code points
	if got := evalOK(t, atLimit, "rad", 0).Display; got != "100" {
		t.Errorf("a 199-character input = %q", got)
	}
	deep := strings.Repeat("(", 51) + "1" + strings.Repeat(")", 51)
	if code := evalCode(t, deep, "rad"); code != CodeTooDeep {
		t.Errorf("51 nested parentheses = %s, want TOO_DEEP", code)
	}
	shallow := strings.Repeat("(", 50) + "1" + strings.Repeat(")", 50)
	if got := evalOK(t, shallow, "rad", 0).Display; got != "1" {
		t.Errorf("50 nested parentheses = %q, want 1", got)
	}
}

func TestDisplay(t *testing.T) {
	for _, test := range []struct{ value, display string }{
		{"0.1+0.2", "0.3"},
		{"1/3", "0.333333333333"},
		{"1e15", "1e+15"},
		{"0.0000001", "1e-7"},
		{"-0", "0"},
		{"0", "0"},
		{"2e3", "2000"},
		{"123456789012345", "1.23456789012e+14"},
		{"0.000001", "0.000001"},
		{"-1/3", "-0.333333333333"},
		{"1e12", "1e+12"},
		{"999999999999", "999999999999"},
	} {
		if got := evalOK(t, test.value, "rad", 0).Display; got != test.display {
			t.Errorf("%q displays %q, want %q", test.value, got, test.display)
		}
	}
}

func TestEmptyAndGarbage(t *testing.T) {
	for _, input := range []string{"", "   ", "2 +", "@", "#", "2 .", "()", "sin(,)", "sin(30", "2e+", "2.", "1.5.3", "1,2"} {
		if code := evalCode(t, input, "rad"); code != CodeSyntax {
			t.Errorf("%q = %s, want SYNTAX", input, code)
		}
	}
}
