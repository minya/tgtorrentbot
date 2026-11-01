package commands

import "errors"

// repliedError marks an error the command has already reported in the chat.
type repliedError struct{ err error }

func (e repliedError) Error() string { return e.err.Error() }
func (e repliedError) Unwrap() error { return e.err }

// Replied wraps err to say the user has already been told about it, so the
// update handler only logs it instead of sending a second, generic reply.
func Replied(err error) error {
	if err == nil {
		return nil
	}
	return repliedError{err}
}

// IsReplied reports whether err (or an error it wraps) was marked Replied.
func IsReplied(err error) bool {
	var r repliedError
	return errors.As(err, &r)
}
