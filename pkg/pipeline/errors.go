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

var (
	ErrSourceNoRepo       = fmt.Errorf("source.repository is required when source is specified")
	ErrCredFileNotFound   = fmt.Errorf("credentials file not found")
	ErrCredFilePerm       = fmt.Errorf("credentials file permissions not 0600")
	ErrCredentialNotFound = fmt.Errorf("no credential found for secret")
	ErrCheckoutFailed     = fmt.Errorf("source checkout failed")
	ErrSecretNotResolved  = fmt.Errorf("secret referenced but not found in credentials")
)
