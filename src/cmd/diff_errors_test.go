package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// gitShowError wraps the git failure, so errors.Is and errors.As reach it
// (CONV-004), also when gitShowError is itself wrapped.
func TestGitShowErrorUnwraps(t *testing.T) {
	cause := errors.New("git failed")
	err := fmt.Errorf("loading base: %w", &gitShowError{ref: "main", path: "x.json", cause: cause})
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is didn't reach the cause through %v", err)
	}

	exitErr := &exec.ExitError{}
	err = fmt.Errorf("loading base: %w", &gitShowError{ref: "main", path: "x.json", cause: exitErr})
	var got *exec.ExitError
	if !errors.As(err, &got) || got != exitErr {
		t.Errorf("errors.As didn't reach the *exec.ExitError through %v", err)
	}
	if !isExitError(err, nil) {
		t.Error("isExitError missed a wrapped *exec.ExitError")
	}
}
