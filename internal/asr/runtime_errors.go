package asr

import "errors"

const ErrorCodeRuntimeUnavailable = "asr_runtime_unavailable"
const ErrorCodeRuntimeStartupFailed = "asr_runtime_startup_failed"
const ErrorCodeRuntimeConfigInvalid = "asr_runtime_config_invalid"
const ErrorCodeModelMissing = "asr_model_missing"
const ErrorCodeEmptyTranscript = "asr_empty_transcript"

type runtimeError interface {
	error
	Code() string
	Diagnostic() string
}

type RuntimeError struct {
	code    string
	message string
	cause   error
	detail  string
}

func (e RuntimeError) Error() string {
	diagnostic := e.Diagnostic()
	if diagnostic == "" {
		return e.message
	}
	return e.message + ": " + diagnostic
}

func (e RuntimeError) Unwrap() error {
	return e.cause
}

func (e RuntimeError) Code() string {
	return e.code
}

func (e RuntimeError) Diagnostic() string {
	if e.detail != "" {
		return e.detail
	}
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
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

func RuntimeErrorDiagnosticOf(err error) string {
	if err == nil {
		return ""
	}
	var coded runtimeError
	if errors.As(err, &coded) {
		return coded.Diagnostic()
	}
	return ""
}

func newRuntimeUnavailableError(message string, cause error) error {
	return RuntimeError{code: ErrorCodeRuntimeUnavailable, message: message, cause: cause}
}

func newRuntimeError(code, message string, cause error) error {
	return RuntimeError{code: code, message: message, cause: cause}
}

func newRuntimeErrorWithDiagnostic(code, message, diagnostic string, cause error) error {
	return RuntimeError{code: code, message: message, cause: cause, detail: diagnostic}
}

func NewRuntimeError(code, message string, cause error) error {
	return newRuntimeError(code, message, cause)
}

func NewRuntimeErrorWithDiagnostic(code, message, diagnostic string, cause error) error {
	return newRuntimeErrorWithDiagnostic(code, message, diagnostic, cause)
}

func IsRuntimeUnavailableError(err error) bool {
	return isRuntimeUnavailable(err)
}
