// SPDX-License-Identifier: AGPL-3.0-only

// Package fail is the error type of core: a code from docs/SPEC.md 11.7
// and one sentence for humans. The API layer turns it into api.Error with
// the matching HTTP status. Messages are full sentences on purpose, so
// they are built here rather than with errors.New.
package fail

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/musakkka/5050cloud/pkg/api"
)

// Error is a user-facing error.
type Error struct {
	Code    string
	Msg     string
	Field   string
	Details any
	// Err is an underlying cause that is logged, never shown.
	Err error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

// Sentence is an error whose text is shown as is.
type Sentence string

func (s Sentence) Error() string { return string(s) }

func newf(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Validation names the offending field.
func Validation(field, format string, args ...any) *Error {
	e := newf(api.CodeValidation, format, args...)
	e.Field = field
	return e
}

func NotFound(format string, args ...any) *Error {
	return newf(api.CodeNotFound, format, args...)
}
func Conflict(format string, args ...any) *Error {
	return newf(api.CodeConflict, format, args...)
}
func Forbidden(format string, args ...any) *Error {
	return newf(api.CodeForbidden, format, args...)
}
func Unauthorized(format string, args ...any) *Error {
	return newf(api.CodeUnauthorized, format, args...)
}
func Unavailable(format string, args ...any) *Error {
	return newf(api.CodeUnavailable, format, args...)
}
func RateLimited(format string, args ...any) *Error {
	return newf(api.CodeRateLimited, format, args...)
}

// Internal hides cause behind a generic sentence.
func Internal(cause error) *Error {
	return &Error{Code: api.CodeInternal, Msg: "Something went wrong on the server. The details are in the 5050d log.", Err: cause}
}

// Wrapf is a plain wrapped error for logs (lower case, Go style).
func Wrapf(err error, format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, err)...)
}

// As returns err as *Error, or nil.
func As(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// Is reports whether err carries code.
func Is(err error, code string) bool {
	if e := As(err); e != nil {
		return e.Code == code
	}
	return false
}

// Status maps a code to its HTTP status.
func Status(code string) int {
	switch code {
	case api.CodeUnauthorized:
		return http.StatusUnauthorized
	case api.CodeForbidden:
		return http.StatusForbidden
	case api.CodeNotFound:
		return http.StatusNotFound
	case api.CodeConflict:
		return http.StatusConflict
	case api.CodeValidation:
		return http.StatusUnprocessableEntity
	case api.CodeRateLimited:
		return http.StatusTooManyRequests
	case api.CodeUnavailable:
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// ToAPI turns any error into the response body and status. Unknown errors
// become "internal" with a generic sentence.
func ToAPI(err error) (int, api.Error) {
	if e := As(err); e != nil {
		return Status(e.Code), api.Error{Error: e.Msg, Code: e.Code, Field: e.Field, Details: e.Details}
	}
	var s Sentence
	if errors.As(err, &s) {
		return http.StatusBadRequest, api.Error{Error: string(s), Code: api.CodeValidation}
	}
	ie := Internal(err)
	return http.StatusInternalServerError, api.Error{Error: ie.Msg, Code: ie.Code}
}
