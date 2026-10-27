package calc

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestValidateAcceptsAValidConfig(t *testing.T) {
	config := Tools{Version: 1, Calculator: &Calculator{
		Enabled:           true,
		Modes:             []string{ModeScientific, ModeProgrammer, ModeNeural},
		AngleMode:         AngleRadians,
		DisabledFunctions: []string{"softmax", "ieee754", "factorial", "pi"},
	}}
	if problems := config.Validate(); len(problems) != 0 {
		t.Fatalf("valid config reported %q", problems)
	}
	// A bare tools object and one without modes or angle are valid; Effective applies the defaults.
	if problems := (Tools{Version: 1}).Validate(); len(problems) != 0 {
		t.Fatalf("empty tools reported %q", problems)
	}
	if problems := (Tools{Version: 1, Calculator: &Calculator{Enabled: true}}).Validate(); len(problems) != 0 {
		t.Fatalf("calculator without modes or angle reported %q", problems)
	}
	// A calculator that is off is validated just the same, so a typo cannot hide behind it.
	if problems := (Tools{Version: 1, Calculator: &Calculator{Modes: []string{"foo"}}}).Validate(); len(problems) != 1 {
		t.Fatalf("off calculator with an unknown mode reported %q", problems)
	}
}

func TestValidateListsEveryProblemAndNamesTheEntry(t *testing.T) {
	config := Tools{Version: 2, Calculator: &Calculator{
		Enabled:           true,
		Modes:             []string{ModeScientific, "quaternion"},
		AngleMode:         "gradians",
		DisabledFunctions: []string{"softmax", "solve"},
	}}
	problems := config.Validate()
	want := []string{
		"tools.version must be 1",
		`tools.calculator.modes: unknown mode "quaternion"`,
		`tools.calculator.angle_mode must be deg, rad or user, not "gradians"`,
		`tools.calculator.disabled_functions: unknown name "solve"`,
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("got %q, want %q", problems, want)
	}
}

func TestValidateAnExplicitlyEmptyModeListFails(t *testing.T) {
	// JSON's [] decodes to an empty, non-nil slice: that must fail. A nil slice (the key absent) must not.
	empty := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{}}}
	if problems := empty.Validate(); len(problems) != 1 {
		t.Fatalf("empty mode list reported %q", problems)
	}
}

func TestValidateAcceptsDisabledNamesOfModesThatAreOff(t *testing.T) {
	// D16: a name of a mode that is not enabled is accepted and has no effect.
	config := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific}, DisabledFunctions: []string{"matmul", "nand"}}}
	if problems := config.Validate(); len(problems) != 0 {
		t.Fatalf("disabled names of off modes reported %q", problems)
	}
}

func TestEffectiveAppliesTheDefaults(t *testing.T) {
	effective := Tools{Version: 1, Calculator: &Calculator{Enabled: true}}.Effective()
	if !effective.On() {
		t.Fatal("an enabled calculator stayed off")
	}
	if modes := effective.Calculator.Modes; !slices.Equal(modes, []string{ModeScientific}) {
		t.Fatalf("modes defaulted to %q", modes)
	}
	if effective.Calculator.AngleMode != AngleUser {
		t.Fatalf("angle mode defaulted to %q", effective.Calculator.AngleMode)
	}
	// Explicit values survive, and the result shares no slice with the receiver.
	config := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeNeural}, AngleMode: AngleDegrees, DisabledFunctions: []string{"mse"}}}
	effective = config.Effective()
	if !slices.Equal(effective.Calculator.Modes, []string{ModeNeural}) || effective.Calculator.AngleMode != AngleDegrees {
		t.Fatalf("explicit values were not kept: %+v", effective.Calculator)
	}
	effective.Calculator.Modes[0] = ModeScientific
	effective.Calculator.DisabledFunctions[0] = "pi"
	if config.Calculator.Modes[0] != ModeNeural || config.Calculator.DisabledFunctions[0] != "mse" {
		t.Fatal("Effective shares slices with the receiver")
	}
}

func TestAllowedFollowsTheRegistryAndTheDisabledList(t *testing.T) {
	config := Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific, ModeProgrammer}, DisabledFunctions: []string{"sinh", "ieee754"}}}
	allowed := Allowed(config.Effective())
	for _, name := range []string{"pi", "e", "log", "sin", "tanh", "factorial", "nand", "rol"} {
		if !allowed[name] {
			t.Fatalf("%s should be allowed", name)
		}
	}
	for _, name := range []string{"sinh", "ieee754", "sigmoid", "matmul"} {
		if allowed[name] {
			t.Fatalf("%s should not be allowed", name)
		}
	}
	// Disabling a constant makes it an unknown identifier, never a function.
	config = Tools{Version: 1, Calculator: &Calculator{Enabled: true, Modes: []string{ModeScientific}, DisabledFunctions: []string{"pi"}}}
	if allowed := Allowed(config.Effective()); allowed["pi"] {
		t.Fatal("a disabled constant stayed allowed")
	}
	// An off or absent calculator allows nothing.
	if allowed := Allowed(Tools{Version: 1, Calculator: &Calculator{}}); len(allowed) != 0 {
		t.Fatalf("an off calculator allowed %d names", len(allowed))
	}
	if allowed := Allowed(Tools{Version: 1}); len(allowed) != 0 {
		t.Fatalf("no calculator allowed %d names", len(allowed))
	}
}

func TestConfigRoundTripsThroughTheJSONContract(t *testing.T) {
	contract := `{"tools":{"version":1,"calculator":{"enabled":true,"modes":["scientific","neural"],"angle_mode":"rad","disabled_functions":["softmax"]}}}`
	var document struct {
		Tools Tools `json:"tools"`
	}
	if err := json.Unmarshal([]byte(contract), &document); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if problems := document.Tools.Validate(); len(problems) != 0 {
		t.Fatalf("the contract sample does not validate: %q", problems)
	}
	effective := document.Tools.Effective()
	if !slices.Equal(effective.Calculator.Modes, []string{ModeScientific, ModeNeural}) || effective.Calculator.AngleMode != AngleRadians {
		t.Fatalf("the contract sample lost its values: %+v", effective.Calculator)
	}
	// A calculator that is off keeps enabled false through the contract, and an absent calculator stays nil.
	var off struct {
		Tools Tools `json:"tools"`
	}
	if err := json.Unmarshal([]byte(`{"tools":{"version":1,"calculator":{"enabled":false}}}`), &off); err != nil {
		t.Fatalf("decode off: %v", err)
	}
	if off.Tools.On() {
		t.Fatal("a calculator with enabled false is on")
	}
	var absent struct {
		Tools Tools `json:"tools"`
	}
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatalf("decode absent: %v", err)
	}
	if absent.Tools.Calculator != nil {
		t.Fatal("a tools object without a calculator grew one")
	}
}
