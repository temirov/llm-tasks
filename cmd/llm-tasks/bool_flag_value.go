package llmtasks

import (
	"fmt"
	"strconv"
	"strings"
)

type boolChoiceValue struct {
	target *bool
}

func newBoolChoiceValue(target *bool) *boolChoiceValue {
	return &boolChoiceValue{target: target}
}

func (value *boolChoiceValue) String() string {
	if value == nil || value.target == nil {
		return ""
	}
	return strconv.FormatBool(*value.target)
}

func (value *boolChoiceValue) Set(input string) error {
	boolValue, ok := parseBoolChoice(input)
	if !ok {
		return fmt.Errorf("invalid boolean value %q", input)
	}
	*value.target = boolValue
	return nil
}

func (value *boolChoiceValue) Type() string {
	return "bool"
}

func parseBoolChoice(input string) (bool, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		trimmed = "true"
	}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "true", "t", "1", "yes", "y", "on":
		return true, true
	case "false", "f", "0", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}
