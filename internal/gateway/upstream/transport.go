package upstream

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
	"github.com/wweir/warden/internal/providerauth"
	sel "github.com/wweir/warden/internal/selector"
)

type compositeReadCloser struct {
	io.Reader
	closers []io.Closer
}

type NonStreamingResponseError struct {
	Body []byte
}

func (e *NonStreamingResponseError) Error() string {
	return "upstream returned non-stream response"
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

const firstTokenTimeout = 30 * time.Second

func SendRequest(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, endpoint string, body []byte, isStreaming bool) ([]byte, time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	if clientReq != nil {
		httpReq.Header = BuildForwardedRequestHeaders(clientReq)
		// This path parses upstream payloads as JSON/SSE.
		// Keep net/http default gzip handling instead of forwarding client compression preferences.
		httpReq.Header.Del("Accept-Encoding")
	}

	providerauth.SetHeaders(ctx, httpReq.Header, provCfg)

	client := provCfg.HTTPClient(0)
	if isStreaming {
		client = provCfg.HTTPClient(firstTokenTimeout)
	}

	upstreamStart := time.Now()
	resp, err := client.Do(httpReq)
	latency := time.Since(upstreamStart)
	if err != nil {
		return nil, latency, fmt.Errorf("send request: %w", err)
	}
	respBody, err := readHTTPResponseBody(resp)
	if err != nil {
		slog.Warn("SendRequest: failed to read response body", "error", err, "status", resp.StatusCode, "bytes_read", len(respBody))
		if isStreaming {
			return nil, latency, fmt.Errorf("read stream body: %w", err)
		}
		return nil, latency, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, latency, &sel.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	if upErr := upstreamBodyError(resp.StatusCode, respBody); upErr != nil {
		return nil, latency, upErr
	}

	return respBody, latency, nil
}

func SendStreamingRequest(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, endpoint string, body []byte) (io.ReadCloser, time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	if clientReq != nil {
		httpReq.Header = BuildForwardedRequestHeaders(clientReq)
		httpReq.Header.Del("Accept-Encoding")
	}

	providerauth.SetHeaders(ctx, httpReq.Header, provCfg)

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
		return nil, latency, &sel.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
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
		if isEventStreamBody(respBody) {
			return io.NopCloser(bytes.NewReader(respBody)), latency, nil
		}
		if upErr := upstreamBodyError(resp.StatusCode, respBody); upErr != nil {
			return nil, latency, upErr
		}
		return nil, latency, &NonStreamingResponseError{Body: respBody}
	}

	return reader, latency, nil
}

func WriteUpstreamAwareError(w http.ResponseWriter, err error) {
	var upErr *sel.UpstreamError
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

	http.Error(w, upErr.Body, upErr.Code)
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

func upstreamBodyError(statusCode int, respBody []byte) *sel.UpstreamError {
	trimmed := strings.TrimSpace(string(respBody))
	if trimmed == "" {
		return nil
	}

	if trimmed[0] == '<' || strings.HasPrefix(trimmed, "<!DOCTYPE") {
		return &sel.UpstreamError{Code: statusCode, Body: trimmed}
	}

	if errType, _ := sel.ParseErrorBody(trimmed); errType != "" && sel.IsRetryableByBody(trimmed) {
		return &sel.UpstreamError{Code: statusCode, Body: trimmed}
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

func isEventStreamBody(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:")
}
