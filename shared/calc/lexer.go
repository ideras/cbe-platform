package calc

import (
	"strconv"
)

// The lexer turns the input into tokens: numbers, identifiers and the single-character operators
// + - * / ^ ! ( ) ,, and the brackets [ ] of a vector or matrix when neural mode is on (Phase 7); without it a
// bracket is a stray character, SYNTAX as before. Whitespace is ignored. Identifiers use the letters a-z, A-Z, _ and the digits: a name is
// looked up as written, so `Sin` is simply unknown, like `foo`.

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokIdent
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokCaret
	tokBang
	tokLParen
	tokRParen
	tokComma
	tokLBracket
	tokRBracket
)

type token struct {
	kind  tokenKind
	text  string  // the identifier's name
	value float64 // the number's value
}

func lex(input string, brackets bool) ([]token, *Error) {
	var tokens []token
	for i := 0; i < len(input); {
		c := input[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case c >= '0' && c <= '9' || c == '.':
			end := scanNumber(input, i)
			if end == i {
				return nil, &Error{Code: CodeSyntax} // a '.' with no digits, or another stray character
			}
			value, err := strconv.ParseFloat(input[i:end], 64)
			if err != nil {
				return nil, &Error{Code: CodeSyntax}
			}
			tokens = append(tokens, token{kind: tokNumber, value: value})
			i = end
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_':
			end := i + 1
			for end < len(input) && isIdentChar(input[end]) {
				end++
			}
			tokens = append(tokens, token{kind: tokIdent, text: input[i:end]})
			i = end
		default:
			kind, ok := operator(c)
			if !ok || !brackets && (kind == tokLBracket || kind == tokRBracket) {
				return nil, &Error{Code: CodeSyntax}
			}
			tokens = append(tokens, token{kind: kind})
			i++
		}
	}
	return tokens, nil
}

func isIdentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func operator(c byte) (tokenKind, bool) {
	switch c {
	case '+':
		return tokPlus, true
	case '-':
		return tokMinus, true
	case '*':
		return tokStar, true
	case '/':
		return tokSlash, true
	case '^':
		return tokCaret, true
	case '!':
		return tokBang, true
	case '(':
		return tokLParen, true
	case ')':
		return tokRParen, true
	case ',':
		return tokComma, true
	case '[':
		return tokLBracket, true
	case ']':
		return tokRBracket, true
	}
	return tokEOF, false
}

// scanNumber consumes a NUMBER: digits with an optional fraction, or a fraction alone, then an optional
// exponent. The exponent belongs to the number only when a digit follows the e (with an optional sign), so
// `2e3` is 2000 while `2e` lexes as the number 2 and the identifier e (implicit multiplication).
func scanNumber(input string, i int) int {
	start := i
	digits := func(i int) int {
		for i < len(input) && input[i] >= '0' && input[i] <= '9' {
			i++
		}
		return i
	}
	if input[i] == '.' {
		i++
		fraction := digits(i)
		if fraction == i {
			return start // '.' alone is no token
		}
		i = fraction
	} else {
		i = digits(i)
		if i < len(input) && input[i] == '.' {
			fraction := digits(i + 1)
			if fraction > i+1 {
				i = fraction
			}
		}
	}
	if i < len(input) && (input[i] == 'e' || input[i] == 'E') {
		j := i + 1
		if j < len(input) && (input[j] == '+' || input[j] == '-') {
			j++
		}
		exponent := digits(j)
		if exponent > j {
			i = exponent
		}
	}
	return i
}
