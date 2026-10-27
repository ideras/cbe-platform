package calc

// The error codes of every phase (Appendix D of the plan), so the frontend's message module can cover them all
// at once. Phases 5, 6A and 7 use the later ones; Phase 2 returns only the first nine.
const (
	CodeSyntax          = "SYNTAX"
	CodeUnknownIdent    = "UNKNOWN_IDENT"
	CodeUnknownFunction = "UNKNOWN_FUNCTION"
	CodeArity           = "ARITY"
	CodeDivisionByZero  = "DIVISION_BY_ZERO"
	CodeDomain          = "DOMAIN"
	CodeOverflow        = "OVERFLOW"
	CodeTooLong         = "TOO_LONG"
	CodeTooDeep         = "TOO_DEEP"
	CodeShiftRange      = "SHIFT_RANGE"
	CodeNotInteger      = "NOT_INTEGER" // an integer-only operator given a real (D20), or a real to decode
	CodeBitWidth        = "BIT_WIDTH"   // a bit pattern wider than the inspected format (D21)
	CodeTypeMismatch    = "TYPE_MISMATCH"
	CodeDimMismatch     = "DIM_MISMATCH"
	CodeTooLarge        = "TOO_LARGE"
)

// Error is a calculator failure: a code, never prose. The frontend maps codes to messages in one module, so the
// engine can stay in English-free Go and the same code means the same thing on every platform.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
