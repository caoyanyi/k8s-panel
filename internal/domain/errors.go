package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("resource not found")
	ErrConflict     = errors.New("resource conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidState = errors.New("invalid resource state")
	ErrBusy         = errors.New("system is busy")
	ErrUpstream     = errors.New("upstream request failed")
	ErrTimeout      = errors.New("request timed out")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validate %s: %s", e.Field, e.Message)
}

func Invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
