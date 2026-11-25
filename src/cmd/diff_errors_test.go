package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// gitError wraps the git failure, so errors.Is and errors.As reach it
// (CONV-004), also when gitError is itself wrapped.
func TestGitErrorUnwraps(t *testing.T) {
	cause := errors.New("git failed")
	err := fmt.Errorf("loading base: %w", &gitError{args: "ls-tree main", cause: cause})
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is didn't reach the cause through %v", err)
	}

	exitErr := &exec.ExitError{}
	err = fmt.Errorf("loading base: %w", &gitError{args: "ls-tree main", cause: exitErr})
	var got *exec.ExitError
	if !errors.As(err, &got) || got != exitErr {
		t.Errorf("errors.As didn't reach the *exec.ExitError through %v", err)
	}
	if !isExitError(err, nil) {
		t.Error("isExitError missed a wrapped *exec.ExitError")
	}
}
