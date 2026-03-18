package gateway

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type encodingQualitySet map[string]float64

func parseAcceptEncoding(header string) (encodingQualitySet, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, false
	}

	qualities := make(encodingQualitySet)
	for _, token := range strings.Split(header, ",") {
		part := strings.TrimSpace(token)
		if part == "" {
			continue
		}

		name := part
		q := 1.0

		if semi := strings.Index(part, ";"); semi >= 0 {
			name = strings.TrimSpace(part[:semi])
			params := strings.Split(part[semi+1:], ";")
			for _, param := range params {
				p := strings.TrimSpace(param)
				if !strings.HasPrefix(strings.ToLower(p), "q=") {
					continue
				}
				parsed, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64)
				if err != nil {
					q = 0
					break
				}
				if parsed < 0 {
					q = 0
					break
				}
				if parsed > 1 {
					q = 1
					break
				}
				q = parsed
				break
			}
		}

		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if old, ok := qualities[name]; !ok || q > old {
			qualities[name] = q
		}
	}
	return qualities, true
}

func encodingQuality(qualities encodingQualitySet, hasHeader bool, name string) float64 {
	if !hasHeader {
		return 0
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if q, ok := qualities[name]; ok {
		return q
	}
	if q, ok := qualities["*"]; ok {
		return q
	}
	return 0
}

func selectAcceptedEncoding(header string, preferred []string) string {
	qualities, hasHeader := parseAcceptEncoding(header)
	if !hasHeader || len(preferred) == 0 {
		return ""
	}

	bestEncoding := ""
	bestQ := 0.0
	for _, enc := range preferred {
		q := encodingQuality(qualities, hasHeader, enc)
		if q <= 0 {
			continue
		}
		if q > bestQ {
			bestQ = q
			bestEncoding = enc
		}
	}
	return bestEncoding
}

func buildUpstreamAcceptEncoding(header string, preferred []string) string {
	qualities, hasHeader := parseAcceptEncoding(header)
	if !hasHeader || len(preferred) == 0 {
		return ""
	}

	accepted := make([]string, 0, len(preferred))
	for _, enc := range preferred {
		if encodingQuality(qualities, hasHeader, enc) > 0 {
			accepted = append(accepted, enc)
		}
	}
	if len(accepted) == 0 {
		return ""
	}
	if len(accepted) == 1 {
		return accepted[0]
	}

	parts := make([]string, 0, len(accepted))
	for i, enc := range accepted {
		if i == 0 {
			parts = append(parts, enc)
			continue
		}
		q := 1.0 - float64(i)*0.1
		if q < 0.1 {
			q = 0.1
		}
		parts = append(parts, fmt.Sprintf("%s;q=%.1f", enc, q))
	}
	return strings.Join(parts, ", ")
}

func negotiateProxyAcceptEncoding(clientHeader string, inferenceEndpoint bool) string {
	preferred := []string{"br", "gzip"}
	if inferenceEndpoint {
		preferred = []string{"zstd", "br", "gzip"}
	}
	return buildUpstreamAcceptEncoding(clientHeader, preferred)
}

func normalizeContentEncoding(value string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), ",")
	if len(parts) == 0 {
		return ""
	}
	token := strings.TrimSpace(parts[0])
	if semi := strings.Index(token, ";"); semi >= 0 {
		token = strings.TrimSpace(token[:semi])
	}
	return token
}

func decodeResponseBody(contentEncoding string, body []byte) ([]byte, error) {
	contentEncoding = normalizeContentEncoding(contentEncoding)
	switch contentEncoding {
	case "", "identity":
		return body, nil
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create gzip reader: %w", err)
		}
		defer gr.Close()
		decoded, err := io.ReadAll(gr)
		if err != nil {
			return nil, fmt.Errorf("read gzip body: %w", err)
		}
		return decoded, nil
	case "br":
		decoded, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
		if err != nil {
			return nil, fmt.Errorf("read brotli body: %w", err)
		}
		return decoded, nil
	case "zstd":
		zr, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create zstd reader: %w", err)
		}
		defer zr.Close()
		decoded, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("read zstd body: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", contentEncoding)
	}
}

func compressedBodyPlaceholder(contentEncoding string, size int) string {
	if contentEncoding == "" {
		contentEncoding = "unknown"
	}
	return fmt.Sprintf("<compressed response encoding=%s bytes=%d>", contentEncoding, size)
}
