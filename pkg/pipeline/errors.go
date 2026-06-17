package pipeline

import (
	"errors"
	"fmt"
)

var (
	ErrEmptyName = errors.New("pipeline name is required")
	ErrNoSteps   = errors.New("pipeline must have at least one step")
)

func ErrStepNameEmpty(index int) error {
	return fmt.Errorf("step at index %d has empty name", index)
}

func ErrStepImageEmpty(name string) error {
	return fmt.Errorf("step %q has empty image", name)
}

func ErrStepNoCommands(name string) error {
	return fmt.Errorf("step %q has no commands", name)
}
