package config

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
)

// SecretString is a string that may be stored as base64 in config files.
// On read, it accepts both base64-encoded and plaintext values.
// On write, it saves as base64-encoded string.
type SecretString string

// Encoded returns the storage form of the secret.
func (s SecretString) Encoded() string {
	if s == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// MarshalText implements encoding.TextMarshaler.
// It encodes the secret as base64 for storage.
func (s SecretString) MarshalText() ([]byte, error) {
	return []byte(s.Encoded()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts both base64-encoded and plaintext values.
func (s *SecretString) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		*s = ""
		return nil
	}

	str := string(data)

	decoded, ok := decodeSecretString(str)
	if ok {
		*s = SecretString(decoded)
		return nil
	}

	*s = SecretString(str)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s SecretString) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Encoded())
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SecretString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return s.UnmarshalText([]byte(raw))
}

// MarshalYAML implements yaml.Marshaler.
func (s SecretString) MarshalYAML() (any, error) {
	return s.Encoded(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (s *SecretString) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == 0 || node.Tag == "!!null" {
		*s = ""
		return nil
	}
	return s.UnmarshalText([]byte(node.Value))
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
	return SecretString(plaintext).Encoded()
}

// NormalizeSecretStorage converts either plaintext or base64 secret input
// into the canonical base64 storage form.
func NormalizeSecretStorage(raw string) string {
	if raw == "" {
		return ""
	}
	return EncodeSecret(DecodeSecret(raw))
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

func decodeSecretString(str string) (string, bool) {
	if str == "" {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return "", false
	}
	if !utf8.Valid(decoded) {
		return "", false
	}
	if base64.StdEncoding.EncodeToString(decoded) != str {
		return "", false
	}
	return string(decoded), true
}

// HookFuncStringToSecretString decodes plaintext or base64 strings into SecretString
// during mapstructure-based config loading.
func HookFuncStringToSecretString() mapstructure.DecodeHookFuncType {
	secretType := reflect.TypeOf(SecretString(""))

	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String || t != secretType {
			return data, nil
		}

		var secret SecretString
		if err := secret.UnmarshalText([]byte(data.(string))); err != nil {
			return nil, err
		}
		return secret, nil
	}
}
