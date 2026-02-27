package usecase

import "fmt"

type ErrorCode string

const (
	ErrorInvalidInput    ErrorCode = "INVALID_INPUT"
	ErrorInvalidQuestion ErrorCode = "INVALID_QUESTION"
	ErrorRateLimited     ErrorCode = "RATE_LIMITED"
	ErrorUpstream        ErrorCode = "UPSTREAM_ERROR"
	ErrorInternal        ErrorCode = "INTERNAL_ERROR"
)

type Error struct {
	Code   ErrorCode
	Reason string
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("usecase: %s (%s)", e.Code, e.Reason)
	}
	return fmt.Sprintf("usecase: %s (%s): %v", e.Code, e.Reason, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newError(code ErrorCode, reason string, err error) *Error {
	return &Error{Code: code, Reason: reason, Err: err}
}
