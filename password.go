package escp

import "log/slog"

// redacted is what a Password prints as.
const redacted = "[redacted]"

// Password is the password of the switch's admin account. It prints as "[redacted]" whatever
// the verb (%v, %+v, %#v, %q, %s), in a structured log (slog), in an encoder (JSON, XML) and in an
// error that formats a Config, so a Config can be logged or wrapped in an error without leaking it. The only way to
// the text is Reveal, which makes every place that needs it easy to find and review.
type Password string

// String returns "[redacted]".
func (Password) String() string { return redacted }

// GoString returns "[redacted]", so %#v does not print the password either.
func (Password) GoString() string { return redacted }

// LogValue makes slog log "[redacted]" instead of the password.
func (Password) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText makes the encoders that honor encoding.TextMarshaler (encoding/json, encoding/xml,
// and the YAML and TOML libraries) write "[redacted]" too, so a Password inside a value that is
// encoded for a log or a debug dump does not leak. The text is not a way to save a password: it
// cannot be read back.
func (Password) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Reveal returns the password itself, to be sent to the switch (or to be handed to a
// configuration the caller writes for the switch). Do not log the result.
func (p Password) Reveal() string { return string(p) }
