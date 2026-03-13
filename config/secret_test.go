package config

import (
	"encoding/base64"
	"testing"
)

func TestSecretString_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantValue string
	}{
		{"empty", "", ""},
		{"plaintext", "my-secret-key", "my-secret-key"},
		{"with special chars", "p@ssw0rd!#$%", "p@ssw0rd!#$%"},
		{"unicode", "密码测试", "密码测试"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := SecretString(tt.input)

			// Marshal to base64
			data, err := s.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText error: %v", err)
			}

			// Verify it's valid base64
			if tt.input != "" {
				if _, err := base64.StdEncoding.DecodeString(string(data)); err != nil {
					t.Errorf("MarshalText did not produce valid base64: %v", err)
				}
			}

			// Unmarshal back
			var s2 SecretString
			if err := s2.UnmarshalText(data); err != nil {
				t.Fatalf("UnmarshalText error: %v", err)
			}

			if s2.Value() != tt.wantValue {
				t.Errorf("roundtrip got %q, want %q", s2.Value(), tt.wantValue)
			}
		})
	}
}

func TestSecretString_UnmarshalText_Plaintext(t *testing.T) {
	// Test that plaintext values are accepted
	var s SecretString
	if err := s.UnmarshalText([]byte("plaintext-secret")); err != nil {
		t.Fatalf("UnmarshalText error: %v", err)
	}
	if s.Value() != "plaintext-secret" {
		t.Errorf("got %q, want %q", s.Value(), "plaintext-secret")
	}
}

func TestSecretString_UnmarshalText_Base64(t *testing.T) {
	// Test that base64 values are decoded
	encoded := base64.StdEncoding.EncodeToString([]byte("secret-value"))
	var s SecretString
	if err := s.UnmarshalText([]byte(encoded)); err != nil {
		t.Fatalf("UnmarshalText error: %v", err)
	}
	if s.Value() != "secret-value" {
		t.Errorf("got %q, want %q", s.Value(), "secret-value")
	}
}

func TestSecretString_String(t *testing.T) {
	s := SecretString("my-secret")
	if s.String() != "***" {
		t.Errorf("String() = %q, want ***", s.String())
	}

	empty := SecretString("")
	if empty.String() != "" {
		t.Errorf("empty String() = %q, want empty", empty.String())
	}
}

func TestEncodeDecodeSecret(t *testing.T) {
	original := "test-secret-123"
	encoded := EncodeSecret(original)

	// Verify encoded is different from original
	if encoded == original {
		t.Error("EncodeSecret should produce different output")
	}

	// Decode and verify
	decoded := DecodeSecret(encoded)
	if decoded != original {
		t.Errorf("DecodeSecret got %q, want %q", decoded, original)
	}

	// Decode plaintext (should return as-is)
	plaintext := "not-base64"
	if DecodeSecret(plaintext) != plaintext {
		t.Error("DecodeSecret should return non-base64 input unchanged")
	}
}