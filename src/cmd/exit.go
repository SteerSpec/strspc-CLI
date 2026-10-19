package cmd

import "errors"

// Process exit codes shared by commands that distinguish failure classes.
// Commands that do not wrap their errors in exitError exit with 1.
const (
	exitOK          = 0
	exitViolations  = 1 // violations in fail_on states (or any plain error)
	exitConfig      = 2 // configuration, rule resolution or diff collection failed
	exitUnavailable = 3 // the AI provider cannot be used (no key, 401/403, unreachable)
)

// exitError carries a process exit code alongside the underlying error.
// Execute unwraps it so that CI can tell "violations found" (1) from
// "configuration broken" (2) and "provider unavailable" (3).
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

// withExitCode wraps err so that Execute exits with code. A nil err is
// returned unchanged.
func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

// exitCodeOf returns the exit code a command error should produce: the
// wrapped code when err is (or wraps) an exitError, 1 for any other error,
// and 0 for nil.
func exitCodeOf(err error) int {
	if err == nil {
		return exitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return exitViolations
}
