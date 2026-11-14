package application

import "errors"

type ErrorKind uint8

const (
	Internal ErrorKind = iota
	Invalid
	NotFound
	Conflict
	Upstream
)

// Error carries a safe public message while retaining the underlying failure.
type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func failure(kind ErrorKind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

func ErrorDetails(err error) (ErrorKind, string) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Kind, appErr.Message
	}
	return Internal, "internal server error"
}
