package calc

import (
	"math/big"
	"strings"
	"testing"
)

// FuzzEvaluate asserts the engine never panics and never hangs on any input, and that a success is a finite
// value of a valid shape within the size limits, with a non-empty display, in scientific and in neural mode.
// There is no loop whose length depends on the input — the parser is linear, the nesting is capped at 50, a
// factorial runs at most 170 multiplications, and a vector or matrix holds at most 64 elements — so a hang
// would come from a grammar mistake this target should expose.
func FuzzEvaluate(f *testing.F) {
	for _, seed := range []string{
		"2+3*4", "-2^2", "2^3^2", "2^-1", "-3!", "2pi", "2e", "2e3", "2e+1", "2e+x", "3(4+1)", "1/2pi",
		"sin(30)", "log(8)", "log(8, 2)", "log10(1000)", "ln(e)", "sqrt(-1)", "mod(-7, 3)", "1/0", "171!",
		"5!!", "ans+1", "(((", "((((((((((((((((((((1))))))))))))))))))))", strings.Repeat("(", 51) + "1" + strings.Repeat(")", 51),
		"Sin(30)", "foo", "1.5.3", "2 3", "2 .", "@", "\xff",
		// Neural mode (Phase 7): literals, shapes, broadcasting and the whole-value functions.
		"[1, 2, 3]", "[[1, 2], [3, 4]]", "[]", "[[1], [2, 3]]", "[[[1]]]", "[1, [2]]", "2[1]", "[1](2)",
		"softmax([1000, 1000])", "matmul([[1,2],[3,4]], [1,1])", "transpose([[1,2,3]])", "[1,2]^2", "2^[1,2]",
		"bce([0.5], [1])", "mse([1,2],[1])", "argmax([1,3,3])", "ans * [1, 2]", "[ans, ans]", "norm([1e300, 1e300])",
	} {
		f.Add(seed)
	}
	allowed := allowedScientific()
	vector := Quantity{Shape: ShapeVector, Data: []float64{1, 2}}
	f.Fuzz(func(t *testing.T, input string) {
		for _, opts := range []Options{
			{AngleMode: "deg", Allowed: allowed, Ans: num(7)},
			{AngleMode: "rad", Allowed: allowed, Ans: num(7)},
			neuralOptions(vector),
		} {
			result, err := Evaluate(input, opts)
			if err != nil {
				continue
			}
			value := result.Value
			if !value.finite() {
				t.Errorf("%q: success with %v", input, value)
			}
			if result.Display == "" {
				t.Errorf("%q: success with an empty display", input)
			}
			switch value.Shape {
			case ShapeNumber:
				if len(value.Data) != 1 {
					t.Errorf("%q: a number with %d elements", input, len(value.Data))
				}
			case ShapeVector:
				if len(value.Data) == 0 || len(value.Data) > maxVector {
					t.Errorf("%q: a vector of %d elements", input, len(value.Data))
				}
			case ShapeMatrix:
				if value.Rows < 1 || value.Cols < 1 || value.Rows > maxMatrix || value.Cols > maxMatrix || len(value.Data) != value.Rows*value.Cols {
					t.Errorf("%q: a %d by %d matrix with %d elements", input, value.Rows, value.Cols, len(value.Data))
				}
			}
			if !opts.Neural && value.Shape != ShapeNumber {
				t.Errorf("%q: a %v without neural mode", input, value.Shape)
			}
		}
	})
}

// FuzzEvaluateProgrammer asserts the programmer evaluator never panics or hangs, nor the Radix Point tab and the
// IEEE 754 inspector's Encode and Decode that read the same input (Phase 6A), and that a success is well formed:
// a real has a display, an integer fits its word. Shift counts are range-checked and reals are bounded in size,
// so no input can drive an unbounded loop.
func FuzzEvaluateProgrammer(f *testing.F) {
	for _, seed := range []string{
		"0xFF + 1", "127 + 1", "0 - 1", "0xFF / 2", "1 << 31", "-8 >> 1", "rol(0x80, 1)", "7 / -2", "-7 % 3",
		"0xFFFFFFFFFFFFFFFF", "1 + 2 << 3", "6 & 3 | 8", "1 | 2 ^ 3", "ff + 1", "nand(0x0F, 0x3C)", "~0", "ans+1",
		"0x", "1 << -1", "256", "0b12", "1.5", "zzz", "(((", "5!",
		"10.625", "2.0 + 1", ".5", "0x1.8", "1.0 / 3", "-0.0", "0d10.5", "1e3", "1.5e3", "1.5 & 1", "ans * ans",
		"0x3C00 | 0x8000", "0x1e3", "1.0 / 0", "1.", "1.2.3", "-(-0.0) * -1.5",
	} {
		f.Add(seed)
	}
	allowed := allowedProgrammer()
	f.Fuzz(func(t *testing.T, input string) {
		for _, signed := range []bool{true, false} {
			for _, base := range []int{10, 16} {
				opts := IntOptions{Word: 64, Signed: signed, Base: base, Allowed: allowed, Ans: Value{Real: big.NewRat(3, 2)}}
				if value, err := EvaluateProgrammer(input, opts); err == nil && value.IsReal() && FormatReal(value.Real, value.NegativeZero) == "" {
					t.Errorf("%q: a real with an empty display", input)
				}
				opts.Ans = Value{Bits: 255}
				_, _ = Fraction(input, opts)
				for _, format := range []string{"binary16", "bfloat16", "binary32", "binary64"} {
					if view, err := Inspect(input, format, DirectionEncode, opts); err == nil && view.Bits == "" {
						t.Errorf("%q: an encoding without bits", input)
					}
					_, _ = Inspect(input, format, DirectionDecode, opts)
				}
			}
		}
	})
}

// FuzzInspect asserts the IEEE inspector's input parser never panics or hangs, in both directions (D21), for
// Phase 6A now and Phase 8's sweep later, and that a success has a class and bits.
func FuzzInspect(f *testing.F) {
	for _, seed := range []string{"1", "0.1", "65520", "1e40", "1e-7", "-0", "-0.0", "0x3C00", "0x3C0", "0x", "abc", "1.2.3",
		"2^-24", "0x1e3", "0x3C00 | 0x8000", "0x412A0000", "1.0 / 3", "70000.0", "0b0011110000000000"} {
		f.Add(seed)
	}
	allowed := allowedProgrammer()
	f.Fuzz(func(t *testing.T, input string) {
		for format := range IEEEFormats {
			for _, direction := range []string{DirectionEncode, DirectionDecode} {
				view, err := Inspect(input, format, direction, IntOptions{Word: 32, Signed: true, Base: 10, Allowed: allowed})
				if err != nil {
					continue
				}
				if view.Class == "" || view.Bits == "" {
					t.Errorf("%q, %s in %s: success without class or bits", input, direction, format)
				}
			}
		}
	})
}
