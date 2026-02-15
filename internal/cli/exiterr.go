package cli

import "fmt"

// ExitError carries a user-facing message and an intended exit code.
// Use it for expected failures (validation, auth, API errors).
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	return e.Msg
}

func exitf(code int, format string, args ...any) *ExitError {
	return &ExitError{Code: code, Msg: fmt.Sprintf(format, args...)}
}
