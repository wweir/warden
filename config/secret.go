package config

import (
	"encoding/base64"
)

// SecretString is a string that may be stored as base64 in config files.
// On read, it accepts both base64-encoded and plaintext values.
// On write, it saves as base64-encoded string.
type SecretString string

// MarshalText implements encoding.TextMarshaler.
// It encodes the secret as base64 for storage.
func (s SecretString) MarshalText() ([]byte, error) {
	if s == "" {
		return []byte(""), nil
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	return []byte(encoded), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts both base64-encoded and plaintext values.
func (s *SecretString) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		*s = ""
		return nil
	}

	str := string(data)

	// Try base64 decode first
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err == nil {
		// Successfully decoded as base64
		*s = SecretString(decoded)
		return nil
	}

	// Not valid base64, treat as plaintext
	*s = SecretString(str)
	return nil
}

// Value returns the underlying string value.
func (s SecretString) Value() string {
	return string(s)
}

// String returns a masked value for safe display.
func (s SecretString) String() string {
	if s == "" {
		return ""
	}
	return "***"
}

// EncodeSecret encodes a plaintext secret to base64.
func EncodeSecret(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(plaintext))
}

// DecodeSecret decodes a base64-encoded secret.
// Returns the input unchanged if it's not valid base64.
func DecodeSecret(encoded string) string {
	if encoded == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return encoded // not base64, return as-is
	}
	return string(decoded)
}
