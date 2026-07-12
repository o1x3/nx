package cli

// ExitError carries a specific process exit code alongside an optional error.
// cmd/nx extracts the code with errors.As; a nil Err means "exit silently with
// this code" (nothing extra is printed to stderr).
type ExitError struct {
	Code int
	Err  error
}

func (e ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e ExitError) Unwrap() error { return e.Err }
