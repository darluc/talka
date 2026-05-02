package protocol

import "fmt"

type CodedError interface {
	error
	Code() ErrorCode
}

type Error struct {
	errCode ErrorCode
	message string
}

func (e Error) Error() string {
	return e.message
}

func (e Error) Code() ErrorCode {
	return e.errCode
}

func NewError(code ErrorCode, format string, args ...any) error {
	return Error{errCode: code, message: fmt.Sprintf(format, args...)}
}

func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}

	if coded, ok := err.(CodedError); ok {
		return coded.Code()
	}

	return ""
}

func (m ErrorMessage) AsError() error {
	return Error{errCode: m.Code, message: m.Message}
}
