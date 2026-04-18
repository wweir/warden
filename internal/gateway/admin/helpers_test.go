package admin

import (
	"errors"
	"testing"
)

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestGenerateAPIKeyReturnsErrorWhenEntropyUnavailable(t *testing.T) {
	t.Parallel()

	previous := apiKeyRandomReader
	apiKeyRandomReader = failingReader{}
	t.Cleanup(func() {
		apiKeyRandomReader = previous
	})

	key, err := GenerateAPIKey()
	if err == nil {
		t.Fatal("GenerateAPIKey() error = nil, want error")
	}
	if key != "" {
		t.Fatalf("GenerateAPIKey() key = %q, want empty", key)
	}
}
