package calc

import (
	"math"
	"strings"
)

// Neural mode (plan Phase 7) shares the scientific evaluator: its values are numbers, vectors and matrices,
// written [1, 2, 3] and [[1, 2], [3, 4]]. The operators work elementwise on operands of the same shape, or
// broadcast a number; * is never the matrix product (matmul is). Unary scientific functions apply elementwise,
// and functions of two or more arguments take numbers only (D17).

// Shape says what a Quantity holds.
type Shape int

const (
	ShapeNumber Shape = iota
	ShapeVector
	ShapeMatrix
)

// The size limits, checked whenever a vector or a matrix is built (TOO_LARGE).
const (
	maxVector = 64
	maxMatrix = 8 // rows and columns
)

// Quantity is one value of the scientific evaluator. The zero value is the number 0, which is where ans starts
// (D10). A vector holds its elements in Data; a matrix holds Rows by Cols elements in Data, row by row.
type Quantity struct {
	Shape      Shape
	Rows, Cols int
	Data       []float64
}

func num(x float64) Quantity { return Quantity{Data: []float64{x}} }

// Number is a number's value; it is 0 for the zero Quantity.
func (q Quantity) Number() float64 {
	if len(q.Data) == 0 {
		return 0
	}
	return q.Data[0]
}

func (q Quantity) isNumber() bool { return q.Shape == ShapeNumber }

// number reads the operand of a function that takes numbers only (D17).
func (q Quantity) number() (float64, *Error) {
	if !q.isNumber() {
		return 0, &Error{Code: CodeTypeMismatch}
	}
	return q.Number(), nil
}

// sameShape is true for two numbers, two vectors of one length, or two matrices of one size.
func sameShape(a, b Quantity) bool {
	return a.Shape == b.Shape && a.Rows == b.Rows && a.Cols == b.Cols && len(a.Data) == len(b.Data)
}

// finite reports whether every element is a real number; the evaluator answers OVERFLOW otherwise.
func (q Quantity) finite() bool {
	if q.isNumber() {
		x := q.Number()
		return !math.IsInf(x, 0) && !math.IsNaN(x)
	}
	for _, x := range q.Data {
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return false
		}
	}
	return true
}

// each applies f to every element, keeping the shape: a unary scientific function on a vector or a matrix.
func (q Quantity) each(f func(float64) (float64, *Error)) (Quantity, *Error) {
	if q.isNumber() {
		x, err := f(q.Number())
		return num(x), err
	}
	out := Quantity{Shape: q.Shape, Rows: q.Rows, Cols: q.Cols, Data: make([]float64, len(q.Data))}
	for i, x := range q.Data {
		value, err := f(x)
		if err != nil {
			return Quantity{}, err
		}
		out.Data[i] = value
	}
	return out, nil
}

// pair combines two operands elementwise: the same shape, or a number broadcast over the other operand.
// Anything else is DIM_MISMATCH.
func pair(a, b Quantity, f func(x, y float64) (float64, *Error)) (Quantity, *Error) {
	switch {
	case a.isNumber() && b.isNumber():
		x, err := f(a.Number(), b.Number())
		return num(x), err
	case a.isNumber():
		return b.each(func(y float64) (float64, *Error) { return f(a.Number(), y) })
	case b.isNumber():
		return a.each(func(x float64) (float64, *Error) { return f(x, b.Number()) })
	case !sameShape(a, b):
		return Quantity{}, &Error{Code: CodeDimMismatch}
	}
	out := Quantity{Shape: a.Shape, Rows: a.Rows, Cols: a.Cols, Data: make([]float64, len(a.Data))}
	for i := range a.Data {
		value, err := f(a.Data[i], b.Data[i])
		if err != nil {
			return Quantity{}, err
		}
		out.Data[i] = value
	}
	return out, nil
}

// list builds the value of a bracket literal from its evaluated elements: numbers make a vector, vectors of
// one length make a matrix (one per row). A matrix inside a literal is nesting deeper than a matrix, and a mix
// of numbers and vectors has no shape: both are TYPE_MISMATCH. Rows of different lengths are DIM_MISMATCH.
func list(elements []Quantity) (Quantity, *Error) {
	first := elements[0]
	for _, element := range elements {
		if element.Shape == ShapeMatrix || element.Shape != first.Shape {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
	}
	if first.isNumber() {
		if len(elements) > maxVector {
			return Quantity{}, &Error{Code: CodeTooLarge}
		}
		out := Quantity{Shape: ShapeVector, Data: make([]float64, len(elements))}
		for i, element := range elements {
			out.Data[i] = element.Number()
		}
		return out, nil
	}
	cols := len(first.Data)
	for _, row := range elements {
		if len(row.Data) != cols {
			return Quantity{}, &Error{Code: CodeDimMismatch}
		}
	}
	if len(elements) > maxMatrix || cols > maxMatrix {
		return Quantity{}, &Error{Code: CodeTooLarge}
	}
	out := Quantity{Shape: ShapeMatrix, Rows: len(elements), Cols: cols, Data: make([]float64, 0, len(elements)*cols)}
	for _, row := range elements {
		out.Data = append(out.Data, row.Data...)
	}
	return out, nil
}

// Display writes a value as the student reads it and as the log records it: a number as Format does, a
// vector as [a, b, c], and a matrix on one line as [[a, b], [c, d]].
func (q Quantity) Display() string {
	switch q.Shape {
	case ShapeVector:
		return "[" + strings.Join(formatAll(q.Data), ", ") + "]"
	case ShapeMatrix:
		rows := make([]string, q.Rows)
		for i, row := range q.Cells() {
			rows[i] = "[" + strings.Join(row, ", ") + "]"
		}
		return "[" + strings.Join(rows, ", ") + "]"
	}
	return Format(q.Number())
}

// Cells is a matrix's elements, formatted, one slice per row, for a display that aligns the columns; it is
// nil for a number or a vector.
func (q Quantity) Cells() [][]string {
	if q.Shape != ShapeMatrix {
		return nil
	}
	rows := make([][]string, q.Rows)
	for i := range rows {
		rows[i] = formatAll(q.Data[i*q.Cols : (i+1)*q.Cols])
	}
	return rows
}

func formatAll(values []float64) []string {
	out := make([]string, len(values))
	for i, x := range values {
		out[i] = Format(x)
	}
	return out
}

// The activations and their derivatives, per element. The sigmoid is written so that neither branch
// exponentiates a large positive number. drelu is 0 at 0 (D7), and dleaky_relu is the slope a there, the
// left-hand derivative like drelu's.

func sigmoid(x float64) float64 {
	if x >= 0 {
		return 1 / (1 + math.Exp(-x))
	}
	e := math.Exp(x)
	return e / (1 + e)
}

func relu(x float64) float64 {
	if x > 0 {
		return x
	}
	return 0
}

func drelu(x float64) float64 {
	if x > 0 {
		return 1
	}
	return 0
}

// defaultLeakySlope is leaky_relu's and dleaky_relu's slope when only x is given.
const defaultLeakySlope = 0.01

// neural computes the functions that read a whole vector or matrix, or take two of them. The checks run in
// order: the kind of each argument (TYPE_MISMATCH), then the shapes against each other (DIM_MISMATCH), then the
// domain.
func neural(name string, args []Quantity) (Quantity, *Error) {
	a := args[0]
	switch name {
	case "softmax":
		if a.Shape != ShapeVector {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
		// The maximum is subtracted first, so softmax([1000, 1000]) is [0.5, 0.5] rather than Inf/Inf.
		top := maxOf(a.Data)
		out := Quantity{Shape: ShapeVector, Data: make([]float64, len(a.Data))}
		total := 0.0
		for i, x := range a.Data {
			out.Data[i] = math.Exp(x - top)
			total += out.Data[i]
		}
		for i := range out.Data {
			out.Data[i] /= total
		}
		return out, nil
	case "sum", "mean", "max", "min":
		if a.isNumber() {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
		switch name {
		case "sum":
			return num(sumOf(a.Data)), nil
		case "mean":
			return num(sumOf(a.Data) / float64(len(a.Data))), nil
		case "max":
			return num(maxOf(a.Data)), nil
		}
		return num(minOf(a.Data)), nil
	case "argmax", "len", "norm":
		if a.Shape != ShapeVector {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
		switch name {
		case "argmax":
			best := 0 // the first of equal maxima
			for i, x := range a.Data {
				if x > a.Data[best] {
					best = i
				}
			}
			return num(float64(best)), nil
		case "len":
			return num(float64(len(a.Data))), nil
		}
		return num(norm(a.Data)), nil
	case "transpose":
		if a.Shape != ShapeMatrix {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
		out := Quantity{Shape: ShapeMatrix, Rows: a.Cols, Cols: a.Rows, Data: make([]float64, len(a.Data))}
		for i := 0; i < a.Rows; i++ {
			for j := 0; j < a.Cols; j++ {
				out.Data[j*a.Rows+i] = a.Data[i*a.Cols+j]
			}
		}
		return out, nil
	case "dot":
		b := args[1]
		if a.Shape != ShapeVector || b.Shape != ShapeVector {
			return Quantity{}, &Error{Code: CodeTypeMismatch}
		}
		if len(a.Data) != len(b.Data) {
			return Quantity{}, &Error{Code: CodeDimMismatch}
		}
		return num(dot(a.Data, b.Data)), nil
	case "matmul":
		return matmul(a, args[1])
	case "mse", "bce":
		return loss(name, a, args[1])
	}
	return Quantity{}, &Error{Code: CodeUnknownFunction}
}

// matmul is the matrix product: a matrix times a matrix is a matrix, and a matrix times a vector is a vector.
// The first argument must be a matrix. The result is never larger than the operands, so it needs no size check.
func matmul(a, b Quantity) (Quantity, *Error) {
	if a.Shape != ShapeMatrix || b.isNumber() {
		return Quantity{}, &Error{Code: CodeTypeMismatch}
	}
	if b.Shape == ShapeVector {
		if a.Cols != len(b.Data) {
			return Quantity{}, &Error{Code: CodeDimMismatch}
		}
		out := Quantity{Shape: ShapeVector, Data: make([]float64, a.Rows)}
		for i := range out.Data {
			out.Data[i] = dot(a.Data[i*a.Cols:(i+1)*a.Cols], b.Data)
		}
		return out, nil
	}
	if a.Cols != b.Rows {
		return Quantity{}, &Error{Code: CodeDimMismatch}
	}
	out := Quantity{Shape: ShapeMatrix, Rows: a.Rows, Cols: b.Cols, Data: make([]float64, a.Rows*b.Cols)}
	for i := 0; i < a.Rows; i++ {
		for j := 0; j < b.Cols; j++ {
			total := 0.0
			for k := 0; k < a.Cols; k++ {
				total += a.Data[i*a.Cols+k] * b.Data[k*b.Cols+j]
			}
			out.Data[i*b.Cols+j] = total
		}
	}
	return out, nil
}

// loss computes mse and bce of two numbers or two vectors of one length. bce always uses the natural log, and
// a prediction outside the open interval (0, 1) is DOMAIN; the targets are not checked.
func loss(name string, p, y Quantity) (Quantity, *Error) {
	if p.Shape == ShapeMatrix || y.Shape == ShapeMatrix || p.Shape != y.Shape {
		return Quantity{}, &Error{Code: CodeTypeMismatch}
	}
	if p.isNumber() {
		p, y = Quantity{Shape: ShapeVector, Data: []float64{p.Number()}}, Quantity{Shape: ShapeVector, Data: []float64{y.Number()}}
	}
	if len(p.Data) != len(y.Data) {
		return Quantity{}, &Error{Code: CodeDimMismatch}
	}
	total := 0.0
	for i, pi := range p.Data {
		yi := y.Data[i]
		if name == "mse" {
			total += (pi - yi) * (pi - yi)
			continue
		}
		if !(pi > 0 && pi < 1) {
			return Quantity{}, &Error{Code: CodeDomain}
		}
		total -= yi*math.Log(pi) + (1-yi)*math.Log1p(-pi)
	}
	return num(total / float64(len(p.Data))), nil
}

func sumOf(values []float64) float64 {
	total := 0.0
	for _, x := range values {
		total += x
	}
	return total
}

func maxOf(values []float64) float64 {
	top := values[0]
	for _, x := range values[1:] {
		top = math.Max(top, x)
	}
	return top
}

func minOf(values []float64) float64 {
	bottom := values[0]
	for _, x := range values[1:] {
		bottom = math.Min(bottom, x)
	}
	return bottom
}

func dot(a, b []float64) float64 {
	total := 0.0
	for i := range a {
		total += a[i] * b[i]
	}
	return total
}

// norm is the L2 norm, scaled by the largest magnitude so that squaring cannot overflow.
func norm(values []float64) float64 {
	scale := 0.0
	for _, x := range values {
		scale = math.Max(scale, math.Abs(x))
	}
	if scale == 0 || math.IsInf(scale, 0) {
		return scale
	}
	total := 0.0
	for _, x := range values {
		total += (x / scale) * (x / scale)
	}
	return scale * math.Sqrt(total)
}
