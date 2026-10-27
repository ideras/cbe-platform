package calc

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// neuralOptions is a calculator with scientific and neural modes and nothing disabled, as the hosts build it.
func neuralOptions(ans Quantity) Options {
	effective := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific, ModeNeural}}}.Effective()
	return Options{AngleMode: AngleRadians, Allowed: Allowed(effective), Ans: ans, Neural: ModeEnabled(effective, ModeNeural)}
}

func neuralOK(t *testing.T, input string) Result {
	t.Helper()
	result, err := Evaluate(input, neuralOptions(Quantity{}))
	if err != nil {
		t.Fatalf("%q: unexpected %v", input, err)
	}
	return result
}

func neuralCode(t *testing.T, input string) string {
	t.Helper()
	result, err := Evaluate(input, neuralOptions(Quantity{}))
	if err == nil {
		t.Fatalf("%q: expected an error, got %s", input, result.Display)
	}
	return err.(*Error).Code
}

// TestNeuralPlanVectors covers the minimum tests of Phase 7.
func TestNeuralPlanVectors(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"sigmoid(0)", "0.5"},
		{"relu(-2)", "0"},
		{"drelu(0)", "0"}, // D7
		{"softmax([1000, 1000])", "[0.5, 0.5]"},
		{"dot([1,2,3], [4,5,6])", "32"},
		{"matmul([[1,2],[3,4]], [[5,6],[7,8]])", "[[19, 22], [43, 50]]"},
		{"matmul([[1,2],[3,4]], [1,1])", "[3, 7]"},
		{"norm([3,4])", "5"},
		{"[1,2]^2", "[1, 4]"},
		{"exp([0, 0])", "[1, 1]"},
		{"bce(0.9, 1)", "0.105360515658"},
	} {
		if got := neuralOK(t, test.input).Display; got != test.display {
			t.Errorf("%q displays %q, want %q", test.input, got, test.display)
		}
	}
	for _, test := range []struct{ input, code string }{
		{"log([1,2], 2)", CodeTypeMismatch},
		{"dot([1,2], [1,2,3])", CodeDimMismatch},
		{"[[1,2],[3]]", CodeDimMismatch},
		{"bce(1, 1)", CodeDomain},
		{"[" + strings.Repeat("1,", 64) + "1]", CodeTooLarge},
	} {
		if got := neuralCode(t, test.input); got != test.code {
			t.Errorf("%q = %s, want %s", test.input, got, test.code)
		}
	}
}

func TestNeuralModeOffKeepsItsNamesUnknown(t *testing.T) {
	opts := Options{AngleMode: AngleRadians, Allowed: allowedScientific()}
	for _, test := range []struct{ input, code string }{
		{"softmax(1)", CodeUnknownFunction},
		{"sigmoid(0)", CodeUnknownFunction},
		{"softmax([1])", CodeSyntax}, // without neural mode a bracket is a stray character, as before Phase 7
		{"[1, 2]", CodeSyntax},
	} {
		_, err := Evaluate(test.input, opts)
		if err == nil || err.(*Error).Code != test.code {
			t.Errorf("%q = %v, want %s", test.input, err, test.code)
		}
	}
	// tanh is shared: it stays a scientific function.
	if got := evalOK(t, "tanh(0)", "rad", 0).Display; got != "0" {
		t.Errorf("tanh(0) = %q", got)
	}
}

// With neural mode alone, the scientific functions are unknown but the operators and brackets work.
func TestNeuralModeAlone(t *testing.T) {
	effective := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeNeural}}}.Effective()
	opts := Options{Allowed: Allowed(effective), Neural: true}
	if result, err := Evaluate("sum([1, 2] * 3) + tanh(0)", opts); err != nil || result.Display != "9" {
		t.Fatalf("got %+v, %v", result, err)
	}
	if _, err := Evaluate("exp([0])", opts); err == nil || err.(*Error).Code != CodeUnknownFunction {
		t.Fatalf("exp in neural alone = %v", err)
	}
	if got := ScientificPanel(effective); got != ModeNeural {
		t.Fatalf("panel = %q", got)
	}
	both := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeNeural, ModeScientific}}}.Effective()
	if got := ScientificPanel(both); got != ModeScientific {
		t.Fatalf("panel = %q", got)
	}
}

func TestLiterals(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"[1+2, sin(0)]", "[3, 0]"},
		{"[[1, 2], [3, 4]]", "[[1, 2], [3, 4]]"},
		{"[[1, 2]]", "[[1, 2]]"},     // a 1 by 2 matrix, not a vector
		{"[[1], [2]]", "[[1], [2]]"}, // a 2 by 1 matrix
		{"[5]", "[5]"},               // a vector of one element
		{"[1, 2](3)", "[3, 6]"},      // implicit multiplication before '(' still holds
		{"[" + strings.Repeat("1,", 63) + "1]", "[" + strings.Repeat("1, ", 63) + "1]"}, // 64 elements fit
	} {
		if got := neuralOK(t, test.input).Display; got != test.display {
			t.Errorf("%q displays %q, want %q", test.input, got, test.display)
		}
	}
	for _, test := range []struct{ input, code string }{
		{"[]", CodeSyntax},
		{"[1, 2", CodeSyntax},
		{"[1,]", CodeSyntax},
		{"[[1,2],[3,4]][1]", CodeSyntax}, // no indexing
		{"2[1, 2]", CodeSyntax},          // no implicit multiplication before '['
		{"[1][2]", CodeSyntax},
		{"[[[1]]]", CodeTypeMismatch},     // deeper than a matrix
		{"[1, [2, 3]]", CodeTypeMismatch}, // a number beside a vector
		{"[[1, 2], 3]", CodeTypeMismatch},
		{"[[1,2,3,4,5,6,7,8,9]]", CodeTooLarge},                  // nine columns
		{"[" + strings.Repeat("[1],", 8) + "[1]]", CodeTooLarge}, // nine rows
		{strings.Repeat("[", 51) + "1" + strings.Repeat("]", 51), CodeTooDeep},
	} {
		if got := neuralCode(t, test.input); got != test.code {
			t.Errorf("%q = %s, want %s", test.input, got, test.code)
		}
	}
}

func TestElementwiseOperators(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"[1, 2] + [3, 4]", "[4, 6]"},
		{"[1, 2] * [3, 4]", "[3, 8]"}, // elementwise, never the matrix product
		{"[[1, 2], [3, 4]] * [[1, 0], [0, 1]]", "[[1, 0], [0, 4]]"},
		{"2 * [1, 2]", "[2, 4]"},
		{"[1, 2] - 1", "[0, 1]"},
		{"1 / [1, 2]", "[1, 0.5]"},
		{"-[1, -2]", "[-1, 2]"},
		{"[[1, 2], [3, 4]] / 2", "[[0.5, 1], [1.5, 2]]"},
		{"[0, 3]!", "[1, 6]"},
		{"[4, 9]^0.5", "[2, 3]"},
		{"sqrt([[4, 9], [16, 25]])", "[[2, 3], [4, 5]]"},
	} {
		if got := neuralOK(t, test.input).Display; got != test.display {
			t.Errorf("%q displays %q, want %q", test.input, got, test.display)
		}
	}
	for _, test := range []struct{ input, code string }{
		{"[1, 2] + [1, 2, 3]", CodeDimMismatch},
		{"[1, 2] + [[1, 2]]", CodeDimMismatch},
		{"[[1, 2], [3, 4]] + [[1, 2, 3], [4, 5, 6]]", CodeDimMismatch},
		{"2 ^ [1, 2]", CodeTypeMismatch},
		{"[1, 2] / [1, 0]", CodeDivisionByZero},
		{"[1, 2] / 0", CodeDivisionByZero},
		{"sqrt([4, -1])", CodeDomain},
		{"[-8, 8]^(1/3)", CodeDomain},
		{"[1.5]!", CodeDomain},
		{"mod([5], 3)", CodeTypeMismatch},
		{"atan2([1], 1)", CodeTypeMismatch},
		{"exp([1000, 0])", CodeOverflow},
		{"[1e308, 1e308] * 10", CodeOverflow},
	} {
		if got := neuralCode(t, test.input); got != test.code {
			t.Errorf("%q = %s, want %s", test.input, got, test.code)
		}
	}
}

func TestNeuralFunctions(t *testing.T) {
	for _, test := range []struct{ input, display string }{
		{"sigmoid([-1000, 0, 1000])", "[0, 0.5, 1]"},
		{"sigmoid(-40)", "4.24835425529e-18"},
		{"dsigmoid(0)", "0.25"},
		{"dtanh(0)", "1"},
		{"tanh([0])", "[0]"},
		{"relu([-1, 0, 2])", "[0, 0, 2]"},
		{"drelu([-1, 0, 2])", "[0, 0, 1]"},
		{"leaky_relu(-2)", "-0.02"},
		{"leaky_relu([-2, 3], 0.1)", "[-0.2, 3]"},
		{"dleaky_relu([-2, 0, 3])", "[0.01, 0.01, 1]"},
		{"dleaky_relu(-1, 0.2)", "0.2"},
		{"softmax([1, 2, 3])", "[0.0900305731704, 0.244728471055, 0.665240955775]"},
		{"softmax([-1000, 0])", "[0, 1]"},
		{"sum([[1, 2], [3, 4]])", "10"},
		{"mean([1, 2, 3, 4])", "2.5"},
		{"max([[1, 9], [3, 4]])", "9"},
		{"min([3, -1, 2])", "-1"},
		{"argmax([1, 3, 3, 2])", "1"}, // the first of equal maxima, 0-based
		{"len([1, 2, 3])", "3"},
		{"norm([1e200, 1e200])", "1.41421356237e+200"}, // no overflow while squaring
		{"norm([0, 0])", "0"},
		{"transpose([[1, 2, 3], [4, 5, 6]])", "[[1, 4], [2, 5], [3, 6]]"},
		{"matmul([[1, 2, 3]], [[1], [1], [1]])", "[[6]]"},
		{"mse(3, 1)", "4"},
		{"mse([1, 2], [1, 4])", "2"},
		{"bce([0.9, 0.1], [1, 0])", "0.105360515658"},
		{"bce(0.5, 0.5)", "0.69314718056"},
	} {
		if got := neuralOK(t, test.input).Display; got != test.display {
			t.Errorf("%q displays %q, want %q", test.input, got, test.display)
		}
	}
	for _, test := range []struct{ input, code string }{
		{"softmax(1)", CodeTypeMismatch},
		{"softmax([[1, 2]])", CodeTypeMismatch},
		{"sum(5)", CodeTypeMismatch},
		{"argmax([[1, 2]])", CodeTypeMismatch},
		{"len(3)", CodeTypeMismatch},
		{"norm([[3, 4]])", CodeTypeMismatch},
		{"transpose([1, 2])", CodeTypeMismatch},
		{"dot(1, 2)", CodeTypeMismatch},
		{"matmul([1, 2], [[1], [2]])", CodeTypeMismatch}, // a vector first
		{"matmul([[1, 2]], 3)", CodeTypeMismatch},
		{"matmul([[1, 2]], [[1, 2]])", CodeDimMismatch},
		{"matmul([[1, 2]], [1, 2, 3])", CodeDimMismatch},
		{"leaky_relu(1, [0.1])", CodeTypeMismatch},
		{"mse(1, [1])", CodeTypeMismatch},
		{"mse([[1]], [[1]])", CodeTypeMismatch},
		{"mse([1, 2], [1])", CodeDimMismatch},
		{"bce([0.5, 0], [1, 0])", CodeDomain},
		{"bce(-0.1, 0)", CodeDomain},
		{"softmax()", CodeArity},
		{"dot([1])", CodeArity},
		{"leaky_relu(1, 2, 3)", CodeArity},
	} {
		if got := neuralCode(t, test.input); got != test.code {
			t.Errorf("%q = %s, want %s", test.input, got, test.code)
		}
	}
}

// A disallowed function answers UNKNOWN_FUNCTION before its arity or its argument kinds.
func TestNeuralDisabledFunction(t *testing.T) {
	effective := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeNeural}, DisabledFunctions: []string{"softmax"}}}.Effective()
	opts := Options{Allowed: Allowed(effective), Neural: true}
	for _, input := range []string{"softmax([1])", "softmax()", "softmax(1, 2)", "softmax(5)"} {
		if _, err := Evaluate(input, opts); err == nil || err.(*Error).Code != CodeUnknownFunction {
			t.Errorf("%q = %v, want UNKNOWN_FUNCTION", input, err)
		}
	}
}

// ans keeps a vector or a matrix (D10), and a literal may be built from it.
func TestAnsHoldsVectorsAndMatrices(t *testing.T) {
	vector := neuralOK(t, "[1, 2, 3]").Value
	result, err := Evaluate("ans * 2", neuralOptions(vector))
	if err != nil || result.Display != "[2, 4, 6]" {
		t.Fatalf("ans * 2 = %+v, %v", result, err)
	}
	result, err = Evaluate("[ans, ans]", neuralOptions(vector))
	if err != nil || result.Display != "[[1, 2, 3], [1, 2, 3]]" {
		t.Fatalf("[ans, ans] = %+v, %v", result, err)
	}
	if got := result.Value.Cells(); len(got) != 2 || strings.Join(got[1], " ") != "1 2 3" {
		t.Fatalf("cells = %v", got)
	}
	if _, err := Evaluate("[ans, 1]", neuralOptions(vector)); err == nil || err.(*Error).Code != CodeTypeMismatch {
		t.Fatalf("[ans, 1] = %v", err)
	}
	// The zero ans is the number 0.
	if result, err := Evaluate("ans + [1]", neuralOptions(Quantity{})); err != nil || result.Display != "[1]" {
		t.Fatalf("zero ans: %+v, %v", result, err)
	}
}

// The display and the Wails result: a matrix carries its rows, a vector and a number do not.
func TestScientificResult(t *testing.T) {
	matrix := ScientificResult(neuralOK(t, "[[1, 2.5], [-3, 4]]"))
	if matrix.Display != "[[1, 2.5], [-3, 4]]" || len(matrix.Rows) != 2 || strings.Join(matrix.Rows[1], ",") != "-3,4" {
		t.Fatalf("matrix result = %+v", matrix)
	}
	if vector := ScientificResult(neuralOK(t, "[1, 2]")); vector.Rows != nil || vector.Display != "[1, 2]" {
		t.Fatalf("vector result = %+v", vector)
	}
	if number := ScientificResult(neuralOK(t, "1/3")); number.Rows != nil || number.Display != "0.333333333333" {
		t.Fatalf("number result = %+v", number)
	}
}

// The longest display a 200-character input can produce stays well under the 4 KiB event detail limit.
func TestLargestDisplayFitsTheEventLimit(t *testing.T) {
	input := "[" + strings.Repeat("1,", 63) + "1]*-1.23456789012e-300"
	if len(input) > maxInputLength {
		t.Fatalf("the input is %d characters", len(input))
	}
	display := neuralOK(t, input).Display
	if len(display) > 1500 {
		t.Fatalf("display is %d bytes", len(display))
	}
}

// softmax sums to 1 and stays finite for any vector the literal limits allow.
func TestSoftmaxIsStable(t *testing.T) {
	for _, scale := range []float64{1, 1e3, 1e100, 1e300} {
		input := "softmax([" + strconv.FormatFloat(scale, 'g', -1, 64) + ", 0, -" + strconv.FormatFloat(scale, 'g', -1, 64) + "])"
		value := neuralOK(t, input).Value
		if total := sumOf(value.Data); math.Abs(total-1) > 1e-12 {
			t.Errorf("%s sums to %v", input, total)
		}
	}
}
