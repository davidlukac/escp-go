package escp

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/davidlukac/escp-go/internal/invalid"
	"github.com/davidlukac/escp-go/model"
)

var (
	// ErrNoReply means the switch did not answer within the timeout. For a write this means it
	// may have been applied: see ErrWriteUnconfirmed.
	ErrNoReply = errors.New("escp: no reply from switch")
	// ErrWriteUnconfirmed means a write was sent and the switch never answered, so it may or may
	// not have been applied. It is returned as an *UnconfirmedWriteError, which also matches
	// ErrNoReply; a read that gets no reply matches only ErrNoReply.
	ErrWriteUnconfirmed = errors.New("escp: the write may have been applied, the switch never answered")
	// ErrReplyTooLarge means a fragmented reply grew past the sane limit.
	ErrReplyTooLarge = errors.New("escp: fragmented reply is larger than any switch sends")
	// ErrRejected means the switch answered a request with a non-zero code: it rejected a write,
	// or refused to report a setting. Wrong credentials are one reason, but any non-zero reply
	// code lands here (a LAG with ports outside its group's range, for example, is refused with
	// code 4 although the credentials are fine); the *RejectedError that wraps it carries the
	// code.
	ErrRejected = errors.New("escp: switch rejected the request")
	// ErrWrongSwitch means the device that answered is not the switch the client talks to: the
	// reply carries another MAC address than Config.MAC or, when that is not set, than the one
	// the switch reported on the client's first contact, or carries none (a real switch puts its
	// MAC in every reply). Nothing was written to it. The
	// *WrongSwitchError that wraps it names both addresses.
	ErrWrongSwitch = errors.New("escp: the device that answered is not the configured switch")
	// ErrSwitchInUse means another Client of this process already talks to that switch (it is
	// named by its MAC address or by its address and port). The switch keeps one session, so two
	// clients of it fail each other; close the first one, or share it. Returned by Dial, and by the
	// first contact of a client whose switch another client holds (see Client.Close).
	ErrSwitchInUse = errors.New("escp: another client of this process already talks to that switch")
	// ErrClosed means the Client was closed: Close ends a client for good, and every later call
	// returns this error without sending anything. Make a new client with Dial.
	ErrClosed = errors.New("escp: the client is closed")
	// ErrNotFound means no switch with the wanted MAC address answered a discovery probe on
	// this network (Locate, or a client configured with a MAC and no working address).
	ErrNotFound = errors.New("escp: no switch with that MAC address answered")
)

// WrongSwitchError is the refusal to talk to a device whose MAC address is not the switch's: Want
// is Config.MAC or, without it, the MAC of the first contact. It matches ErrWrongSwitch.
type WrongSwitchError struct {
	Want, Got model.MAC
	IP        netip.Addr // where the other device answered from; invalid when unknown
}

// Error names both addresses and where the other device was.
func (e *WrongSwitchError) Error() string {
	where := ""
	if e.IP.IsValid() {
		where = " at " + e.IP.String()
	}
	if e.Got.IsZero() {
		return fmt.Sprintf("%s: the device%s answered without a MAC address, the configuration names %s", ErrWrongSwitch.Error(), where, e.Want)
	}
	return fmt.Sprintf("%s: the device%s has MAC %s, the configuration names %s", ErrWrongSwitch.Error(), where, e.Got, e.Want)
}

// Is makes errors.Is(err, ErrWrongSwitch) true.
func (e *WrongSwitchError) Is(target error) bool { return target == ErrWrongSwitch }

// UnconfirmedWriteError is a write that was sent (Config.Retries+1 times, and again after a new
// session key unless it is a management write) without any reply: the switch may have applied it
// once or several times, or never received it. It matches ErrWriteUnconfirmed and ErrNoReply. Read
// the setting back before deciding what to do; writing again is safe for an absolute value, and
// the methods that move the management address settle the outcome themselves.
type UnconfirmedWriteError struct {
	Method model.Method // the write that was not confirmed
	Err    error        // the transport error, which matches ErrNoReply
}

// Error names the write and carries the transport error.
func (e *UnconfirmedWriteError) Error() string {
	return fmt.Sprintf("%s: %s: %v", ErrWriteUnconfirmed.Error(), e.Method, e.Err)
}

// Unwrap returns the transport error, so that errors.Is(err, ErrNoReply) is true.
func (e *UnconfirmedWriteError) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrWriteUnconfirmed) true.
func (e *UnconfirmedWriteError) Is(target error) bool { return target == ErrWriteUnconfirmed }

// invalidf builds an error that matches ErrInvalid and reads as the formatted message.
func invalidf(format string, args ...any) error {
	return invalid.Errorf(format, args...)
}

// ReplyCode is the result code of a switch reply: zero when the request was accepted,
// otherwise the code the switch gives for refusing it. Only some codes have a known cause
// (see RejectedError.Reason); the rest are reported as they came.
type ReplyCode int32

// RejectedError is a request the switch answered with a non-zero code: a write it refused, a
// read it would not answer, or the session key it did not accept. It matches ErrRejected
// (errors.Is) for callers that treat every refusal alike, and names the code for those that
// want to know which one it was. Reason, when set, says why the switch refused (only for codes
// whose cause has been established, and for the refused reads and keys, whose cause is the
// request itself).
type RejectedError struct {
	Code   ReplyCode
	Reason string
}

// Error names the code and, when the client knows it, the reason; for an unknown code it says only that
// the credentials, the setting or the value may be at fault, because the switch does not say which.
func (e *RejectedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s (error code %d): %s", ErrRejected.Error(), e.Code, e.Reason)
	}
	return fmt.Sprintf("%s (error code %d): wrong credentials, or the switch refuses this setting or value", ErrRejected.Error(), e.Code)
}

// Is makes errors.Is(err, ErrRejected) true for every rejected write.
func (e *RejectedError) Is(target error) bool { return target == ErrRejected }

// ErrInvalid means the caller's argument was refused before any write was sent: a port or
// id out of range, a rate that is not a multiple of the step, a combination the switch
// cannot keep. The message names the value and what is allowed; test for the class with
// errors.Is(err, ErrInvalid). It is never returned for a reply the client cannot parse,
// nor for a failure of the switch. It is the value of model.ErrInvalid, which the model's own
// validators return.
var ErrInvalid = model.ErrInvalid
