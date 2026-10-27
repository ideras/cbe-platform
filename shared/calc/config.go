package calc

import (
	"fmt"
	"slices"
)

// The angle modes of the scientific mode. user means the student toggles between deg and rad.
const (
	AngleDegrees = "deg"
	AngleRadians = "rad"
	AngleUser    = "user"
)

// Tools defines an exam's versioned tool configuration. A nil Calculator disables it.
type Tools struct {
	// Version must be 1.
	Version    int         `json:"version"`
	Calculator *Calculator `json:"calculator,omitempty"`
}

// Calculator defines an exam's calculator settings.
type Calculator struct {
	// Enabled turns the calculator on or off.
	Enabled bool `json:"enabled"`
	// Modes lists enabled modes; nil defaults to scientific, while empty is invalid.
	Modes []string `json:"modes,omitempty"`
	// AngleMode is deg, rad, or user; empty defaults to user.
	AngleMode string `json:"angle_mode,omitempty"`
	// DisabledFunctions lists registry entries unavailable to students.
	DisabledFunctions []string `json:"disabled_functions,omitempty"`
}

var (
	modeSet      = map[string]bool{ModeScientific: true, ModeProgrammer: true, ModeNeural: true}
	angleModeSet = map[string]bool{AngleDegrees: true, AngleRadians: true, AngleUser: true}
)

// ModeKnown reports whether mode is a valid calculator mode.
func ModeKnown(mode string) bool { return modeSet[mode] }

// AngleModeKnown reports whether mode is a valid angle mode.
func AngleModeKnown(mode string) bool { return angleModeSet[mode] }

// Validate reports all configuration errors with their fields and invalid values.
// An absent calculator is valid.
func (t Tools) Validate() []string {
	var problems []string
	if t.Version != 1 {
		problems = append(problems, "tools.version must be 1")
	}
	if t.Calculator == nil {
		return problems
	}
	c := t.Calculator
	if c.Modes != nil && len(c.Modes) == 0 {
		problems = append(problems, "tools.calculator.modes is empty; name at least one mode or leave it out for scientific alone")
	}
	for _, mode := range c.Modes {
		if !modeSet[mode] {
			problems = append(problems, fmt.Sprintf("tools.calculator.modes: unknown mode %q", mode))
		}
	}
	if c.AngleMode != "" && !angleModeSet[c.AngleMode] {
		problems = append(problems, fmt.Sprintf("tools.calculator.angle_mode must be deg, rad or user, not %q", c.AngleMode))
	}
	for _, name := range c.DisabledFunctions {
		if !RegistryHas(name) {
			problems = append(problems, fmt.Sprintf("tools.calculator.disabled_functions: unknown name %q", name))
		}
	}
	return problems
}

// Effective applies defaults and returns the evaluator-ready configuration.
// It does not share slices with the receiver.
func (t Tools) Effective() Tools {
	if t.Calculator == nil {
		return Tools{Version: 1}
	}
	c := *t.Calculator
	c.Modes = slices.Clone(c.Modes)
	if len(c.Modes) == 0 {
		c.Modes = []string{ModeScientific}
	}
	if c.AngleMode == "" {
		c.AngleMode = AngleUser
	}
	c.DisabledFunctions = slices.Clone(c.DisabledFunctions)
	return Tools{Version: 1, Calculator: &c}
}

// On reports whether the calculator is enabled; a nil configuration has none.
func (t *Tools) On() bool {
	return t != nil && t.Calculator != nil && t.Calculator.Enabled
}

// ModeEnabled reports whether an effective configuration enables a mode.
func ModeEnabled(effective Tools, mode string) bool {
	return effective.On() && slices.Contains(effective.Calculator.Modes, mode)
}

// ScientificPanel names the panel that evaluates scientific and neural expressions, which share one evaluator
// and one panel (owner, 2026-10-25): "scientific" when the exam enables scientific mode, "neural" when it
// enables neural mode alone. The panel's events and its ans go by this name.
func ScientificPanel(effective Tools) string {
	if !ModeEnabled(effective, ModeScientific) && ModeEnabled(effective, ModeNeural) {
		return ModeNeural
	}
	return ModeScientific
}

// Allowed returns evaluable names for an effective configuration.
// It excludes disabled and pseudo entries; a disabled calculator returns an empty set.
func Allowed(effective Tools) map[string]bool {
	allowed := map[string]bool{}
	if !effective.On() {
		return allowed
	}
	enabled := map[string]bool{}
	for _, mode := range effective.Calculator.Modes {
		enabled[mode] = true
	}
	disabled := map[string]bool{}
	for _, name := range effective.Calculator.DisabledFunctions {
		disabled[name] = true
	}
	for name, entry := range Registry {
		if entry.Pseudo || disabled[name] {
			continue
		}
		for _, mode := range entry.Modes {
			if enabled[mode] {
				allowed[name] = true
				break
			}
		}
	}
	return allowed
}
