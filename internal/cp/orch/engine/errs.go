// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"errors"
	"fmt"
)

// sentenceError carries an error a person reads: one full sentence that
// starts with a capital letter and ends with a full stop (docs/SPEC.md 9.18).
type sentenceError struct{ err error }

func (e sentenceError) Error() string { return e.err.Error() }
func (e sentenceError) Unwrap() error { return errors.Unwrap(e.err) }

// errorf formats a sentence error; %w wraps as with fmt.Errorf.
func errorf(format string, a ...any) error { return sentenceError{fmt.Errorf(format, a...)} }
