package calc

import (
	"math"
	"strconv"
	"strings"
)

// Format turns a result into what the student reads: full precision stays in the value, and the display rounds
// to 12 significant digits. Values with 1e-6 ≤ |v| < 1e12 are fixed; anything bigger or smaller is scientific
// with the exponent written plainly (1e+15, 1e-7). Negative zero reads as 0.
//
// strconv.FormatFloat(v, 'g', 12, 64) is not used as-is: it switches to scientific notation below 1e-4 and
// writes two-digit exponents (1e-07).
func Format(v float64) string {
	if v == 0 {
		return "0" // also -0
	}
	abs := math.Abs(v)
	if abs >= 1e-6 && abs < 1e12 {
		places := 11 - int(math.Floor(math.Log10(abs)))
		if places < 0 {
			places = 0
		}
		return trimZeros(strconv.FormatFloat(v, 'f', places, 64))
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(v, 'e', 11, 64), "e")
	return trimZeros(mantissa) + "e" + exponent[:1] + strings.TrimLeft(exponent[1:], "0")
}

// trimZeros removes the trailing zeros a fixed digit count wrote, and a point left bare by that.
func trimZeros(number string) string {
	if strings.Contains(number, ".") {
		number = strings.TrimRight(number, "0")
		number = strings.TrimRight(number, ".")
	}
	return number
}
