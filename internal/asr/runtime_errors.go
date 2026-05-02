package asr

import "errors"

const ErrorCodeRuntimeUnavailable = "asr_runtime_unavailable"

type runtimeError interface {
	error
	Code() string
}

type RuntimeError struct {
	code    string
	message string
	cause   error
}

func (e RuntimeError) Error() string {
	if e.cause == nil {
		return e.message
	}
	return e.message + ": " + e.cause.Error()
}

func (e RuntimeError) Unwrap() error {
	return e.cause
}

func (e RuntimeError) Code() string {
	return e.code
}

func RuntimeErrorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	var coded runtimeError
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

func newRuntimeUnavailableError(message string, cause error) error {
	return RuntimeError{code: ErrorCodeRuntimeUnavailable, message: message, cause: cause}
}

func IsRuntimeUnavailableError(err error) bool {
	return isRuntimeUnavailable(err)
}
