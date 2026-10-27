// Package calc defines the calculator function registry and exam tool configuration.
//
// It is the single source of truth for available functions, calculator modes,
// and the rules that determine which functions students may use.
package calc

// The calculator's modes. A registry name belongs to one or more of them, and an exam enables any subset.
const (
	ModeScientific = "scientific"
	ModeProgrammer = "programmer"
	ModeNeural     = "neural"
)

// Entry defines a registry name, its modes, argument limits, and availability.
type Entry struct {
	// Modes enables the name when any listed mode is active.
	Modes []string
	// MinArgs and MaxArgs define the allowed argument count.
	MinArgs, MaxArgs int
	// Pseudo names are valid only in disabled_functions, never in Allowed.
	Pseudo bool
}

// Registry of tool names used for configuration and disabled-name validation.
var Registry = map[string]Entry{
	// Constants (scientific, arity 0, used bare; disabling one makes it an unknown identifier).
	"pi": {Modes: []string{ModeScientific}},
	"e":  {Modes: []string{ModeScientific}},
	// Logarithms (D2): log10 base 10, ln natural, log takes base and argument.
	"log10": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"ln":    {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"log":   {Modes: []string{ModeScientific}, MinArgs: 2, MaxArgs: 2},
	// Powers and simple functions.
	"sqrt":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"cbrt":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"exp":   {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"abs":   {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"floor": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"ceil":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"round": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1}, // halves away from zero (D14)
	"mod":   {Modes: []string{ModeScientific}, MinArgs: 2, MaxArgs: 2}, // sign of the dividend (D14)
	// Trigonometry, angle-mode aware.
	"sin":   {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"cos":   {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"tan":   {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"asin":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"acos":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"atan":  {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"atan2": {Modes: []string{ModeScientific}, MinArgs: 2, MaxArgs: 2},
	// Hyperbolic, not angle-mode aware.
	"sinh": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	"cosh": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	// Shared with neural mode.
	"tanh": {Modes: []string{ModeScientific, ModeNeural}, MinArgs: 1, MaxArgs: 1},
	// The postfix !, named so a teacher can disable it (D16).
	"factorial": {Modes: []string{ModeScientific}, MinArgs: 1, MaxArgs: 1},
	// Programmer mode.
	"nand": {Modes: []string{ModeProgrammer}, MinArgs: 2, MaxArgs: 2},
	"nor":  {Modes: []string{ModeProgrammer}, MinArgs: 2, MaxArgs: 2},
	"rol":  {Modes: []string{ModeProgrammer}, MinArgs: 2, MaxArgs: 2},
	"ror":  {Modes: []string{ModeProgrammer}, MinArgs: 2, MaxArgs: 2},
	// Neural mode: activations and their derivatives (the ReLU derivative is 0 at 0, D7).
	"sigmoid":     {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"relu":        {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"leaky_relu":  {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 2}, // slope defaults to 0.01
	"dsigmoid":    {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"dtanh":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"drelu":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"dleaky_relu": {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 2},
	// Neural mode: reductions, softmax, shapes and losses (bce logs naturally).
	"softmax":   {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"sum":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"mean":      {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"max":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"min":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"argmax":    {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"len":       {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"norm":      {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"transpose": {Modes: []string{ModeNeural}, MinArgs: 1, MaxArgs: 1},
	"dot":       {Modes: []string{ModeNeural}, MinArgs: 2, MaxArgs: 2},
	"matmul":    {Modes: []string{ModeNeural}, MinArgs: 2, MaxArgs: 2},
	"mse":       {Modes: []string{ModeNeural}, MinArgs: 2, MaxArgs: 2},
	"bce":       {Modes: []string{ModeNeural}, MinArgs: 2, MaxArgs: 2},
	// The IEEE 754 inspector (D13): a panel of programmer mode, never callable.
	"ieee754": {Modes: []string{ModeProgrammer}, Pseudo: true},
}

// RegistryHas reports whether name exists in the registry, including pseudo-entries.
func RegistryHas(name string) bool {
	_, ok := Registry[name]
	return ok
}
