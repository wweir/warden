package gateway

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/selector"
)

type compositeReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (c *compositeReadCloser) Close() error {
	var closeErr error
	for i := len(c.closers) - 1; i >= 0; i-- {
		if err := c.closers[i].Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

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
	respBody, err := readHTTPResponseBody(resp)
	if err != nil {
		slog.Warn("sendRequest: failed to read response body", "error", err, "status", resp.StatusCode, "bytes_read", len(respBody))
		// For streaming requests, partial data is not useful - return error
		if isStreaming {
			return nil, latency, fmt.Errorf("read stream body: %w", err)
		}
		return nil, latency, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	if upErr := upstreamBodyError(resp.StatusCode, respBody); upErr != nil {
		return nil, latency, upErr
	}

	return respBody, latency, nil
}

func sendStreamingRequest(ctx context.Context, provCfg *config.ProviderConfig, endpoint string, body []byte) (io.ReadCloser, time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	if clientReq, ok := clientRequestFromContext(ctx); ok {
		httpReq.Header = buildForwardedRequestHeaders(clientReq)
		httpReq.Header.Del("Accept-Encoding")
	}

	selector.SetAuthHeaders(httpReq.Header, provCfg)

	client := provCfg.HTTPClient(firstTokenTimeout)
	upstreamStart := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(upstreamStart)
	if err != nil {
		return nil, latency, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := readHTTPResponseBody(resp)
		if readErr != nil {
			return nil, latency, fmt.Errorf("read error response body: %w", readErr)
		}
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	reader, err := responseBodyReader(resp)
	if err != nil {
		resp.Body.Close()
		return nil, latency, err
	}

	if !isEventStreamContentType(resp.Header.Get("Content-Type")) {
		respBody, readErr := readAllAndClose(reader)
		if readErr != nil {
			return nil, latency, fmt.Errorf("read non-stream response body: %w", readErr)
		}
		if upErr := upstreamBodyError(resp.StatusCode, respBody); upErr != nil {
			return nil, latency, upErr
		}
		return nil, latency, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	return reader, latency, nil
}

func responseBodyReader(resp *http.Response) (io.ReadCloser, error) {
	buffered := bufio.NewReader(resp.Body)
	shouldGunzip := strings.EqualFold(strings.TrimSpace(resp.Header.Get("Content-Encoding")), "gzip")
	if !shouldGunzip {
		prefix, err := buffered.Peek(2)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("peek response body: %w", err)
		}
		shouldGunzip = len(prefix) >= 2 && prefix[0] == 0x1f && prefix[1] == 0x8b
	}

	if !shouldGunzip {
		return &compositeReadCloser{
			Reader:  buffered,
			closers: []io.Closer{resp.Body},
		}, nil
	}

	gr, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}

	return &compositeReadCloser{
		Reader:  gr,
		closers: []io.Closer{gr, resp.Body},
	}, nil
}

func readAllAndClose(reader io.ReadCloser) ([]byte, error) {
	defer reader.Close()

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return respBody, err
	}
	return maybeDecompressBody(respBody), nil
}

func readHTTPResponseBody(resp *http.Response) ([]byte, error) {
	reader, err := responseBodyReader(resp)
	if err != nil {
		return nil, err
	}
	return readAllAndClose(reader)
}

func maybeDecompressBody(respBody []byte) []byte {
	// Some proxies omit Content-Encoding even though the body is still gzipped.
	if len(respBody) < 2 || respBody[0] != 0x1f || respBody[1] != 0x8b {
		return respBody
	}

	gr, err := gzip.NewReader(bytes.NewReader(respBody))
	if err != nil {
		return respBody
	}
	defer gr.Close()

	decompressed, readErr := io.ReadAll(gr)
	if readErr != nil {
		return respBody
	}
	return decompressed
}

func upstreamBodyError(statusCode int, respBody []byte) *selector.UpstreamError {
	trimmed := strings.TrimSpace(string(respBody))
	if trimmed == "" {
		return nil
	}

	if trimmed[0] == '<' || strings.HasPrefix(trimmed, "<!DOCTYPE") {
		return &selector.UpstreamError{Code: statusCode, Body: trimmed}
	}

	if errType, _ := selector.ParseErrorBody(trimmed); errType != "" && selector.IsRetryableByBody(trimmed) {
		return &selector.UpstreamError{Code: statusCode, Body: trimmed}
	}
	return nil
}

func isEventStreamContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")
	}
	return mediaType == "text/event-stream"
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
