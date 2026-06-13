package core

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorKindInvalidEvent ErrorKind = "invalid_event"
	ErrorKindAdapter      ErrorKind = "adapter"
	ErrorKindDisabled     ErrorKind = "disabled"
)

var (
	ErrInvalidEvent = errors.New("invalid normalized event")
	ErrAdapter      = errors.New("source adapter error")
	ErrDisabled     = errors.New("proofswe disabled")
)

type ProofsweError struct {
	kind ErrorKind
	msg  string
	err  error
}

func NewError(kind ErrorKind, msg string, err error) error {
	if err == nil {
		err = sentinelForKind(kind)
	}
	return &ProofsweError{
		kind: kind,
		msg:  msg,
		err:  err,
	}
}

func (e *ProofsweError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.msg == "" && e.err == nil {
		return string(e.kind)
	}
	if e.err == nil {
		return e.msg
	}
	if e.msg == "" {
		return e.err.Error()
	}
	return fmt.Sprintf("%s: %v", e.msg, e.err)
}

func (e *ProofsweError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *ProofsweError) Kind() ErrorKind {
	if e == nil {
		return ""
	}
	return e.kind
}

func (e *ProofsweError) Is(target error) bool {
	return target == sentinelForKind(e.kind)
}

func sentinelForKind(kind ErrorKind) error {
	switch kind {
	case ErrorKindInvalidEvent:
		return ErrInvalidEvent
	case ErrorKindAdapter:
		return ErrAdapter
	case ErrorKindDisabled:
		return ErrDisabled
	default:
		return nil
	}
}
