package pairing

import "errors"

var (
	ErrLocalIdentityNotFound = errors.New("local identity not found")
	ErrInvalidPIN            = errors.New("invalid pin")
	ErrExpiredPIN            = errors.New("pin expired")
	ErrRateLimited           = errors.New("pairing rate limited")
	ErrUnknownDevice         = errors.New("unknown device")
)
