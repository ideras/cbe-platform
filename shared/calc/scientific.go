package calc

import (
	"math"
	"unicode/utf8"
)

// Evaluate evaluates one scientific expression: numbers, the ans identifier, the allowed functions of the
// registry, and the operators + - * / ^ (power) and the postfix !. With neural mode on (opts.Neural), values
// may also be vectors and matrices (Phase 7, see neural.go). It returns a *Error with a Code on failure.
//
// opts.Allowed is the set of allowed names, from Allowed(Effective(config)); an empty or nil set allows only
// ans. opts.AngleMode is "deg" or "rad" — anything else behaves as "rad"; the "user" mode is resolved by the
// caller, never here. Full precision is kept internally; Display rounds for reading only.
type Options struct {
	AngleMode string
	Allowed   map[string]bool
	Ans       Quantity
	// Neural accepts the bracket literals of vectors and matrices; the hosts set it when the exam enables
	// neural mode (ModeEnabled). Without it a bracket is SYNTAX, as before Phase 7.
	Neural bool
}

type Result struct {
	Value   Quantity
	Display string
}

// Evaluate checks the limits, parses and evaluates the input. The checks of a call run in order: is the name
// allowed (UNKNOWN_FUNCTION), then the arity (ARITY), then the kinds and shapes of the arguments
// (TYPE_MISMATCH, DIM_MISMATCH), then the domain — so a disallowed function never answers ARITY, and nothing in
// an error reveals which functions exist.
func Evaluate(input string, opts Options) (Result, error) {
	if utf8.RuneCountInString(input) > maxInputLength {
		return Result{}, &Error{Code: CodeTooLong}
	}
	tokens, lexErr := lex(input, opts.Neural)
	if lexErr != nil {
		return Result{}, lexErr
	}
	if len(tokens) == 0 {
		return Result{}, &Error{Code: CodeSyntax}
	}
	expression, parseErr := parse(tokens)
	if parseErr != nil {
		return Result{}, parseErr
	}
	ev := evaluator{deg: opts.AngleMode == AngleDegrees, allowed: opts.Allowed, ans: opts.Ans}
	value, evalErr := ev.eval(expression)
	if evalErr != nil {
		return Result{}, evalErr
	}
	if !value.finite() {
		return Result{}, &Error{Code: CodeOverflow}
	}
	return Result{Value: value, Display: value.Display()}, nil
}

type evaluator struct {
	deg     bool
	allowed map[string]bool
	ans     Quantity
}

func (ev *evaluator) eval(n *node) (Quantity, *Error) {
	switch n.kind {
	case nodeNumber:
		return num(n.value), nil
	case nodeIdent:
		return ev.ident(n.text)
	case nodeUnary:
		value, err := ev.eval(n.argv[0])
		if err != nil {
			return Quantity{}, err
		}
		if n.operator == tokMinus {
			return value.each(func(x float64) (float64, *Error) { return -x, nil })
		}
		return value, nil
	case nodeBinary:
		left, err := ev.eval(n.argv[0])
		if err != nil {
			return Quantity{}, err
		}
		right, err := ev.eval(n.argv[1])
		if err != nil {
			return Quantity{}, err
		}
		return binary(n.operator, left, right)
	case nodeFactorial:
		if !ev.allowed["factorial"] {
			return Quantity{}, &Error{Code: CodeUnknownFunction}
		}
		value, err := ev.eval(n.argv[0])
		if err != nil {
			return Quantity{}, err
		}
		return value.each(factorial) // a unary scientific function, elementwise like the others (D17)
	case nodeCall:
		return ev.call(n)
	case nodeList:
		elements := make([]Quantity, len(n.argv))
		for i, element := range n.argv {
			value, err := ev.eval(element)
			if err != nil {
				return Quantity{}, err
			}
			elements[i] = value
		}
		return list(elements)
	}
	return Quantity{}, &Error{Code: CodeSyntax}
}

func (ev *evaluator) ident(name string) (Quantity, *Error) {
	if name == "ans" {
		return ev.ans, nil
	}
	entry, ok := Registry[name]
	if !ok || entry.Pseudo || !ev.allowed[name] || entry.MinArgs != 0 || entry.MaxArgs != 0 {
		return Quantity{}, &Error{Code: CodeUnknownIdent}
	}
	value, err := constant(name)
	return num(value), err
}

// A bare name is a value only when it is an allowed constant; everything else — a function name without its
// parentheses included — is an unknown identifier, the same error `foo` gives.
func constant(name string) (float64, *Error) {
	switch name {
	case "pi":
		return math.Pi, nil
	case "e":
		return math.E, nil
	}
	return 0, &Error{Code: CodeUnknownIdent}
}

// binary applies an operator elementwise (neural.go's pair). The exponent of ^ must be a number (D17).
func binary(operator tokenKind, left, right Quantity) (Quantity, *Error) {
	switch operator {
	case tokPlus:
		return pair(left, right, func(x, y float64) (float64, *Error) { return x + y, nil })
	case tokMinus:
		return pair(left, right, func(x, y float64) (float64, *Error) { return x - y, nil })
	case tokStar:
		return pair(left, right, func(x, y float64) (float64, *Error) { return x * y, nil })
	case tokSlash:
		return pair(left, right, func(x, y float64) (float64, *Error) {
			if y == 0 {
				return 0, &Error{Code: CodeDivisionByZero}
			}
			return x / y, nil
		})
	case tokCaret:
		exponent, err := right.number()
		if err != nil {
			return Quantity{}, err
		}
		return left.each(func(x float64) (float64, *Error) { return power(x, exponent) })
	}
	return Quantity{}, &Error{Code: CodeSyntax}
}

func power(base, exponent float64) (float64, *Error) {
	if base < 0 && exponent != math.Trunc(exponent) {
		// (-8)^(1/3) is not -2 in the floats; the calculator refuses rather than answer a complex number.
		return 0, &Error{Code: CodeDomain}
	}
	return math.Pow(base, exponent), nil
}

func factorial(value float64) (float64, *Error) {
	if value < 0 || value != math.Trunc(value) {
		return 0, &Error{Code: CodeDomain}
	}
	if value > 170 {
		return 0, &Error{Code: CodeOverflow}
	}
	result := 1.0
	for i := 2; i <= int(value); i++ {
		result *= float64(i)
	}
	if math.IsInf(result, 0) {
		return 0, &Error{Code: CodeOverflow}
	}
	return result, nil
}

// The neural functions that read a whole vector or matrix, or two of them; neural.go computes them.
var wholeValue = map[string]bool{
	"softmax": true, "sum": true, "mean": true, "max": true, "min": true, "argmax": true, "len": true, "norm": true,
	"transpose": true, "dot": true, "matmul": true, "mse": true, "bce": true,
}

func (ev *evaluator) call(n *node) (Quantity, *Error) {
	if !ev.allowed[n.text] {
		return Quantity{}, &Error{Code: CodeUnknownFunction}
	}
	entry, ok := Registry[n.text]
	if !ok || entry.Pseudo {
		return Quantity{}, &Error{Code: CodeUnknownFunction}
	}
	if len(n.argv) < entry.MinArgs || len(n.argv) > entry.MaxArgs {
		return Quantity{}, &Error{Code: CodeArity}
	}
	arguments := make([]Quantity, len(n.argv))
	for i, argument := range n.argv {
		value, err := ev.eval(argument)
		if err != nil {
			return Quantity{}, err
		}
		arguments[i] = value
	}
	switch {
	case wholeValue[n.text]:
		return neural(n.text, arguments)
	case n.text == "leaky_relu" || n.text == "dleaky_relu":
		// The slope is a number; x may be anything, elementwise.
		slope := defaultLeakySlope
		if len(arguments) == 2 {
			value, err := arguments[1].number()
			if err != nil {
				return Quantity{}, err
			}
			slope = value
		}
		return arguments[0].each(func(x float64) (float64, *Error) { return ev.function(n.text, []float64{x, slope}) })
	case len(arguments) == 1:
		return arguments[0].each(func(x float64) (float64, *Error) { return ev.function(n.text, []float64{x}) })
	}
	// A function of two or more arguments takes numbers only (D17).
	numbers := make([]float64, len(arguments))
	for i, argument := range arguments {
		value, err := argument.number()
		if err != nil {
			return Quantity{}, err
		}
		numbers[i] = value
	}
	value, err := ev.function(n.text, numbers)
	return num(value), err
}

// function computes an allowed call's value on numbers, one element at a time for a vector or a matrix; every
// domain failure of Appendix D but bce's comes from here.
func (ev *evaluator) function(name string, x []float64) (float64, *Error) {
	switch name {
	case "pi":
		return math.Pi, nil
	case "e":
		return math.E, nil
	case "log10":
		if x[0] <= 0 {
			return 0, &Error{Code: CodeDomain}
		}
		return math.Log10(x[0]), nil
	case "ln":
		if x[0] <= 0 {
			return 0, &Error{Code: CodeDomain}
		}
		return math.Log(x[0]), nil
	case "log":
		if x[0] <= 0 {
			return 0, &Error{Code: CodeDomain}
		}
		if x[1] <= 0 || x[1] == 1 {
			return 0, &Error{Code: CodeDomain}
		}
		return math.Log(x[0]) / math.Log(x[1]), nil
	case "sqrt":
		if x[0] < 0 {
			return 0, &Error{Code: CodeDomain}
		}
		return math.Sqrt(x[0]), nil
	case "cbrt":
		return math.Cbrt(x[0]), nil
	case "exp":
		return math.Exp(x[0]), nil
	case "abs":
		return math.Abs(x[0]), nil
	case "floor":
		return math.Floor(x[0]), nil
	case "ceil":
		return math.Ceil(x[0]), nil
	case "round":
		// Halves away from zero (D14): round(2.5) is 3, round(-2.5) is -3.
		return math.Round(x[0]), nil
	case "mod":
		// The sign of the dividend (D14), like Go's math.Mod and the programmer '%'.
		if x[1] == 0 {
			return 0, &Error{Code: CodeDivisionByZero}
		}
		return math.Mod(x[0], x[1]), nil
	case "sin":
		return ev.sin(x[0]), nil
	case "cos":
		return ev.cos(x[0]), nil
	case "tan":
		return ev.tan(x[0])
	case "asin":
		if x[0] < -1 || x[0] > 1 {
			return 0, &Error{Code: CodeDomain}
		}
		return ev.toDegrees(math.Asin(x[0])), nil
	case "acos":
		if x[0] < -1 || x[0] > 1 {
			return 0, &Error{Code: CodeDomain}
		}
		return ev.toDegrees(math.Acos(x[0])), nil
	case "atan":
		return ev.toDegrees(math.Atan(x[0])), nil
	case "atan2":
		return ev.toDegrees(math.Atan2(x[0], x[1])), nil
	case "sinh":
		return math.Sinh(x[0]), nil
	case "cosh":
		return math.Cosh(x[0]), nil
	case "tanh":
		return math.Tanh(x[0]), nil
	case "sigmoid":
		return sigmoid(x[0]), nil
	case "dsigmoid":
		value := sigmoid(x[0])
		return value * (1 - value), nil
	case "dtanh":
		value := math.Tanh(x[0])
		return 1 - value*value, nil
	case "relu":
		return relu(x[0]), nil
	case "drelu":
		return drelu(x[0]), nil
	case "leaky_relu":
		if x[0] > 0 {
			return x[0], nil
		}
		return x[1] * x[0], nil
	case "dleaky_relu":
		if x[0] > 0 {
			return 1, nil
		}
		return x[1], nil
	}
	return 0, &Error{Code: CodeUnknownFunction}
}

// In degrees, trig inputs are first reduced modulo 360, and an exact multiple of 90 gives an exact result: no
// float noise stands in for 0, 1 or -1, and tan of an odd multiple of 90 is a domain error, never a huge
// number. In radians no correction is made; sin(pi) keeps its 1.22e-16 (Appendix B).

func (ev *evaluator) sin(x float64) float64 {
	if !ev.deg {
		return math.Sin(x)
	}
	x = math.Mod(x, 360)
	if k, whole := quadrant(x); whole {
		return sinTable[k]
	}
	return math.Sin(x * math.Pi / 180)
}

func (ev *evaluator) cos(x float64) float64 {
	if !ev.deg {
		return math.Cos(x)
	}
	x = math.Mod(x, 360)
	if k, whole := quadrant(x); whole {
		return cosTable[k]
	}
	return math.Cos(x * math.Pi / 180)
}

func (ev *evaluator) tan(x float64) (float64, *Error) {
	if !ev.deg {
		return math.Tan(x), nil
	}
	x = math.Mod(x, 360)
	if k, whole := quadrant(x); whole {
		if k%2 == 1 {
			return 0, &Error{Code: CodeDomain}
		}
		return 0, nil
	}
	return math.Tan(x * math.Pi / 180), nil
}

func (ev *evaluator) toDegrees(radians float64) float64 {
	if ev.deg {
		return radians * 180 / math.Pi
	}
	return radians
}

// quadrant says which quadrant a reduced degree value sits in: k is the value in nineties when it is exact.
func quadrant(degrees float64) (k int, whole bool) {
	if q := degrees / 90; q == math.Trunc(q) {
		return int(((int(q) % 4) + 4) % 4), true
	}
	return 0, false
}

var (
	sinTable = [4]float64{0, 1, 0, -1}
	cosTable = [4]float64{1, 0, -1, 0}
)
