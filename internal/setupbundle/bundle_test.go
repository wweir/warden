package setupbundle

import (
	"bytes"
	"testing"
)

func TestBuildAndExtract(t *testing.T) {
	bootstrap := []byte("bootstrap")
	payload := []byte("payload-data")

	bundle := Build(bootstrap, payload)
	got, err := Extract(bundle)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Extract() = %q, want %q", got, payload)
	}
}

func TestExtractRejectsMissingTrailer(t *testing.T) {
	if _, err := Extract([]byte("plain-executable")); err == nil {
		t.Fatal("Extract() error = nil, want error")
	}
}
