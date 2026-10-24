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

// errCompleted ends a rollout whose replicas all ran to completion: they
// exited with code 0 and the restart policy does not bring them back. The
// deployment finishes as Completed, not Failed (a job or a one-shot image
// such as hello-world).
var errCompleted = errors.New("ran to completion")

// completedText is the diagnosis shown on a Completed deployment.
const completedText = "The container ran and exited with code 0. Nothing is wrong: this image runs once and stops. " +
	"For a long-running service, use an image that keeps a process running, or set a cron schedule to run it again."
