package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
)

// ConfigFile is the switch's binary configuration file, as ConfigBackup returns it. It holds
// the account block of the switch (obfuscated, not safe), so it prints as its size and hash and
// never as its bytes, under every fmt verb and in slog; convert it to []byte for the bytes. An
// encoder (encoding/json) writes the bytes of a []byte type as base64: encode Base64() on purpose,
// and keep the file out of values that are logged.
type ConfigFile []byte

// Size is the length of the file in bytes.
func (f ConfigFile) Size() int { return len(f) }

// SHA256 is the hex SHA-256 of the file: the hash that tells whether the configuration changed.
func (f ConfigFile) SHA256() string {
	sum := sha256.Sum256(f)
	return hex.EncodeToString(sum[:])
}

// Base64 is the file in standard base64, for a place that can only hold text.
func (f ConfigFile) Base64() string { return base64.StdEncoding.EncodeToString(f) }

// String describes the file by its size and hash, not by its content.
func (f ConfigFile) String() string {
	return fmt.Sprintf("configuration file (%d bytes, sha256 %s)", f.Size(), f.SHA256())
}

// GoString is String, so %#v does not print the bytes either.
func (f ConfigFile) GoString() string { return f.String() }

// LogValue makes slog log the size and hash instead of the bytes.
func (f ConfigFile) LogValue() slog.Value { return slog.StringValue(f.String()) }
