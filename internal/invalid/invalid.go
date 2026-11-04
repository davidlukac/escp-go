// Package invalid is the one error class of refused arguments, shared by the packages of the module
// that validate them: escp and escp/model each return errors that match escp.ErrInvalid, which is
// Err here.
package invalid

import (
	"errors"
	"fmt"
)

// Err is the class every refused argument matches (errors.Is); escp.ErrInvalid is this value.
var Err = errors.New("escp: invalid argument")

type invalidError struct{ msg string }

func (e *invalidError) Error() string { return e.msg }

// Is makes errors.Is(err, Err) true for every refused argument.
func (e *invalidError) Is(target error) bool { return target == Err }

// Errorf builds an error that matches Err and reads as the formatted message.
func Errorf(format string, args ...any) error {
	return &invalidError{msg: fmt.Sprintf(format, args...)}
}
