package gateway

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

// firstTokenTimeout is the fixed timeout for streaming requests.
// Streaming responses should return the first token quickly (typically within seconds).
// This is a fixed value to ensure consistent behavior across all streaming requests.
// Non-streaming requests use the provider's configured timeout instead.
const firstTokenTimeout = 30 * time.Second

// sendRequest sends a raw request body to the upstream endpoint and returns the raw response body
// along with the first-token latency (time from request start to receiving response headers).
// For streaming requests, a fixed 30s first-token timeout is used; for non-streaming, the provider's
// configured timeout applies.
func sendRequest(ctx context.Context, provCfg *config.ProviderConfig, endpoint string, body []byte, isStreaming bool) ([]byte, time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	if clientReq, ok := clientRequestFromContext(ctx); ok {
		httpReq.Header = buildForwardedRequestHeaders(clientReq)
		// This path parses upstream payloads as JSON/SSE.
		// Keep net/http default gzip handling instead of forwarding client compression preferences.
		httpReq.Header.Del("Accept-Encoding")
	}

	selector.SetAuthHeaders(httpReq.Header, provCfg)

	// Use fixed short timeout for streaming, configured timeout for non-streaming
	var client *http.Client
	if isStreaming {
		client = provCfg.HTTPClient(firstTokenTimeout)
	} else {
		client = provCfg.HTTPClient(0) // use configured timeout
	}

	upstreamStart := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(upstreamStart) // first-token latency
	if err != nil {
		return nil, latency, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	reader := resp.Body
	// Manually decompress gzip when the transport did not handle it
	// (e.g. upstream returns gzip without proper Content-Encoding header).
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, latency, fmt.Errorf("create gzip reader: %w", gzErr)
		}
		defer gr.Close()
		reader = gr
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		slog.Warn("sendRequest: failed to read response body", "error", err, "status", resp.StatusCode, "bytes_read", len(respBody))
		// For streaming requests, partial data is not useful - return error
		if isStreaming {
			return nil, latency, fmt.Errorf("read stream body: %w", err)
		}
		return nil, latency, fmt.Errorf("read response body: %w", err)
	}

	// Fallback: if body still looks like gzip (magic bytes 0x1f 0x8b),
	// decompress it. Some proxies omit Content-Encoding header.
	if len(respBody) >= 2 && respBody[0] == 0x1f && respBody[1] == 0x8b {
		gr, gzErr := gzip.NewReader(bytes.NewReader(respBody))
		if gzErr == nil {
			decompressed, readErr := io.ReadAll(gr)
			gr.Close()
			if readErr == nil {
				respBody = decompressed
			}
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	// detect HTML body on 200 (misconfigured proxy returning HTML instead of JSON)
	if trimmed := strings.TrimSpace(string(respBody)); len(trimmed) > 0 && (trimmed[0] == '<' || strings.HasPrefix(trimmed, "<!DOCTYPE")) {
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: trimmed}
	}

	// detect error in HTTP 200 response body (some APIs return errors with 200 status)
	if errType, _ := selector.ParseErrorBody(string(respBody)); errType != "" && selector.IsRetryableByBody(string(respBody)) {
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, latency, nil
}

func writeUpstreamAwareError(w http.ResponseWriter, err error) {
	var upErr *selector.UpstreamError
	if !errors.As(err, &upErr) || upErr.Code < http.StatusBadRequest {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	body := strings.TrimSpace(upErr.Body)
	if len(body) > 0 && (body[0] == '{' || body[0] == '[') {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upErr.Code)
		if _, writeErr := w.Write([]byte(body)); writeErr != nil {
			slog.Warn("Failed to write upstream error response", "error", writeErr)
		}
		return
	}

	http.Error(w, upErr.Error(), upErr.Code)
}

// pipeRawStream sends a raw request body upstream and returns the response bytes
// after writing them to the client. Uses streaming timeout (30s first-token).
func pipeRawStream(ctx context.Context, w http.ResponseWriter, provCfg *config.ProviderConfig, endpoint string, body []byte) ([]byte, error) {
	rawBody, _, err := sendRequest(ctx, provCfg, endpoint, body, true)
	// Always write the response body to the client if it exists
	if rawBody != nil {
		clientBody := rawBody
		if provCfg.Protocol == "anthropic" {
			clientBody = anthropic.ConvertStreamToOpenAI(rawBody)
		}
		if _, writeErr := w.Write(clientBody); writeErr != nil {
			slog.Warn("Failed to write stream response", "error", writeErr)
		}
		w.(http.Flusher).Flush()
	}
	// Always return the raw body even if there's an error
	return rawBody, err
}
