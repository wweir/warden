package gateway

import (
	"testing"

	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
)

func TestSelectAcceptedEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		preferred []string
		want      string
	}{
		{
			name:      "prefer zstd when available",
			header:    "gzip, br, zstd",
			preferred: []string{"zstd", "br", "gzip"},
			want:      "zstd",
		},
		{
			name:      "fallback to br when zstd missing",
			header:    "gzip, br",
			preferred: []string{"zstd", "br", "gzip"},
			want:      "br",
		},
		{
			name:      "respect q value disable",
			header:    "br;q=0, gzip;q=1",
			preferred: []string{"br", "gzip"},
			want:      "gzip",
		},
		{
			name:      "no header no encoding",
			header:    "",
			preferred: []string{"br"},
			want:      "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := upstreampkg.SelectAcceptedEncoding(tt.header, tt.preferred)
			if got != tt.want {
				t.Fatalf("selectAcceptedEncoding() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildUpstreamAcceptEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		preferred []string
		want      string
	}{
		{
			name:      "build ordered accept list",
			header:    "gzip, br, zstd",
			preferred: []string{"zstd", "br", "gzip"},
			want:      "zstd, br;q=0.9, gzip;q=0.8",
		},
		{
			name:      "remove unsupported encodings",
			header:    "gzip",
			preferred: []string{"zstd", "br", "gzip"},
			want:      "gzip",
		},
		{
			name:      "empty when client has no accept header",
			header:    "",
			preferred: []string{"zstd", "br", "gzip"},
			want:      "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := upstreampkg.BuildUpstreamAcceptEncoding(tt.header, tt.preferred)
			if got != tt.want {
				t.Fatalf("buildUpstreamAcceptEncoding() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeContentEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "zstd", want: "zstd"},
		{name: "with params", input: "br; level=5", want: "br"},
		{name: "with chain", input: "gzip, br", want: "gzip"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := upstreampkg.NormalizeContentEncoding(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeContentEncoding() = %q, want %q", got, tt.want)
			}
		})
	}
}
