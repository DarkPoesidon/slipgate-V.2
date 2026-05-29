package actions

import "fmt"

// ActionError is an error with a user-facing hint.
type ActionError struct {
	Action  string
	Message string
	Hint    string
	Err     error
}

func (e *ActionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Action, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Action, e.Message)
}

func (e *ActionError) Unwrap() error { return e.Err }

// NewError creates an ActionError.
func NewError(action, message string, err error) *ActionError {
	return &ActionError{Action: action, Message: message, Err: err}
}

// NewErrorWithHint creates an ActionError with a user-facing hint.
func NewErrorWithHint(action, message, hint string, err error) *ActionError {
	return &ActionError{Action: action, Message: message, Hint: hint, Err: err}
}
