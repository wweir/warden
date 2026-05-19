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
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/providerauth"
	sel "github.com/wweir/warden/internal/selector"
)

type compositeReadCloser struct {
	io.Reader
	closers []io.Closer
}

type firstTokenReadCloser struct {
	body     io.ReadCloser
	deadline *FirstTokenDeadline
}

type NonStreamingResponseError struct {
	Body []byte
}

type FirstTokenTimeoutError struct {
	Duration time.Duration
}

type FirstTokenDeadline struct {
	started time.Time
	cancel  context.CancelFunc
	timer   *time.Timer

	mu      sync.Mutex
	stopped bool
	timed   bool
	latency time.Duration
}

func (e *NonStreamingResponseError) Error() string {
	return "upstream returned non-stream response"
}

func (e *FirstTokenTimeoutError) Error() string {
	return fmt.Sprintf("first token timeout after %s", e.Duration)
}

func (e *FirstTokenTimeoutError) Timeout() bool {
	return true
}

func (e *FirstTokenTimeoutError) Temporary() bool {
	return true
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

func (r *firstTokenReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 || err != nil {
		r.deadline.Stop()
	}
	return n, err
}

func (r *firstTokenReadCloser) Close() error {
	r.deadline.Stop()
	defer r.deadline.Cancel()
	return r.body.Close()
}

func NewFirstTokenDeadline(parent context.Context, timeout time.Duration) (context.Context, *FirstTokenDeadline) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	deadline := &FirstTokenDeadline{
		started: time.Now(),
		cancel:  cancel,
	}
	deadline.timer = time.AfterFunc(timeout, deadline.timeout)
	return ctx, deadline
}

func (d *FirstTokenDeadline) timeout() {
	shouldCancel := false
	d.mu.Lock()
	if !d.stopped {
		d.timed = true
		shouldCancel = true
	}
	d.mu.Unlock()
	if shouldCancel {
		d.cancel()
	}
}

func (d *FirstTokenDeadline) Stop() time.Duration {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return d.latency
	}
	d.stopped = true
	d.latency = time.Since(d.started)
	if d.timer != nil {
		d.timer.Stop()
	}
	return d.latency
}

func (d *FirstTokenDeadline) Cancel() {
	if d == nil || d.cancel == nil {
		return
	}
	d.cancel()
}

func (d *FirstTokenDeadline) Latency() time.Duration {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return d.latency
	}
	return time.Since(d.started)
}

func (d *FirstTokenDeadline) TimedOut() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.timed
}

func (d *FirstTokenDeadline) TimeoutError(timeout time.Duration) error {
	if d != nil && d.TimedOut() {
		return &FirstTokenTimeoutError{Duration: timeout}
	}
	return nil
}

func DoWithFirstTokenTimeout(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, *FirstTokenDeadline, error) {
	ctx, deadline := NewFirstTokenDeadline(req.Context(), timeout)
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		deadline.Stop()
		deadline.Cancel()
		if timeoutErr := deadline.TimeoutError(timeout); timeoutErr != nil {
			return nil, deadline, timeoutErr
		}
		return nil, deadline, err
	}
	resp.Body = &firstTokenReadCloser{body: resp.Body, deadline: deadline}
	return resp, deadline, nil
}

// JoinBaseURLPath joins a base URL with a request path.
// If path is already absolute, it is returned unchanged.
func JoinBaseURLPath(baseURL, reqPath string) string {
	baseURL = strings.TrimSpace(baseURL)
	reqPath = strings.TrimSpace(reqPath)
	if reqPath == "" {
		return baseURL
	}
	if parsed, err := url.Parse(reqPath); err == nil && parsed.IsAbs() {
		return reqPath
	}
	if baseURL == "" {
		return reqPath
	}

	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		if strings.HasPrefix(reqPath, "/") {
			return strings.TrimRight(baseURL, "/") + reqPath
		}
		return strings.TrimRight(baseURL, "/") + "/" + reqPath
	}
	if strings.HasPrefix(reqPath, "/") {
		parsedBase.Path = path.Join(parsedBase.Path, reqPath)
	} else {
		parsedBase.Path = path.Join(parsedBase.Path, "/"+reqPath)
	}
	return parsedBase.String()
}

func waitForFirstToken(reader io.ReadCloser, deadline *FirstTokenDeadline, timeout time.Duration) (io.ReadCloser, time.Duration, error) {
	buffered := bufio.NewReader(reader)
	if _, err := buffered.Peek(1); err != nil {
		latency := deadline.Latency()
		_ = reader.Close()
		return nil, latency, wrapFirstTokenReadError(deadline, timeout, "read first token", err)
	}
	latency := deadline.Stop()
	return &compositeReadCloser{
		Reader:  buffered,
		closers: []io.Closer{reader},
	}, latency, nil
}

func SendRequest(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, endpoint string, body []byte, isStreaming bool) ([]byte, time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	resp, deadline, err := sendUpstream(ctx, clientReq, provCfg, endpoint, body)
	latency := deadline.Latency()
	if err != nil {
		return nil, latency, err
	}
	timeout := provCfg.FirstTokenTimeout(0)
	readContext := "read response body"
	if isStreaming {
		readContext = "read stream body"
	}
	respBody, latency, err := readHTTPResponseBodyWithDeadline(resp, deadline, timeout, readContext)
	if err != nil {
		slog.Warn("SendRequest: failed to read response body", "error", err, "status", resp.StatusCode, "bytes_read", len(respBody))
		return nil, latency, err
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

	resp, deadline, err := sendUpstream(ctx, clientReq, provCfg, endpoint, body)
	latency := deadline.Latency()
	if err != nil {
		return nil, latency, err
	}
	timeout := provCfg.FirstTokenTimeout(0)

	if resp.StatusCode != http.StatusOK {
		respBody, latency, readErr := readHTTPResponseBodyWithDeadline(resp, deadline, timeout, "read error response body")
		if readErr != nil {
			return nil, latency, readErr
		}
		return nil, latency, &sel.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	reader, err := responseBodyReader(resp)
	latency = deadline.Latency()
	if err != nil {
		resp.Body.Close()
		return nil, latency, wrapFirstTokenReadError(deadline, timeout, "prepare response body", err)
	}

	if !isEventStreamContentType(resp.Header.Get("Content-Type")) {
		respBody, latency, readErr := readAllAndCloseWithDeadline(reader, deadline, timeout, "read non-stream response body")
		if readErr != nil {
			return nil, latency, readErr
		}
		if isEventStreamBody(respBody) {
			return io.NopCloser(bytes.NewReader(respBody)), latency, nil
		}
		if upErr := upstreamBodyError(resp.StatusCode, respBody); upErr != nil {
			return nil, latency, upErr
		}
		return nil, latency, &NonStreamingResponseError{Body: respBody}
	}

	reader, latency, err = waitForFirstToken(reader, deadline, timeout)
	if err != nil {
		return nil, latency, err
	}
	return reader, latency, nil
}

// buildUpstreamRequest constructs the POST request to the upstream provider,
// applies header sanitization and provider authentication. Auth failures return
// a sanitized *sel.UpstreamError so callers can surface a 401 without leaking
// internal details.
func buildUpstreamRequest(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, endpoint string, body []byte) (*http.Request, error) {
	targetURL := JoinBaseURLPath(provCfg.URL, endpoint)
	return buildUpstreamRequestAtURL(ctx, clientReq, provCfg, targetURL, body)
}

func buildUpstreamRequestAtURL(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, targetURL string, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if clientReq != nil {
		httpReq.Header = BuildForwardedRequestHeaders(clientReq)
		if provCfg.Backend == config.ProviderBackendCLIProxy {
			SanitizeCLIProxyRequestHeaders(httpReq.Header)
		}
		// This path parses upstream payloads as JSON/SSE.
		// Keep net/http default gzip handling instead of forwarding client compression preferences.
		httpReq.Header.Del("Accept-Encoding")
	}
	if err := providerauth.SetHeaders(ctx, httpReq.Header, provCfg, targetURL); err != nil {
		return nil, &sel.UpstreamError{Code: http.StatusUnauthorized, Body: providerauth.ClientAuthFailureBody}
	}
	slog.Debug("build upstream request", "url", targetURL)
	return httpReq, nil
}

// sendUpstream sends the request once. Do not replay POST requests here: Go's
// transport already handles the cases it can prove are safe, and exposed idle
// connection errors may still have written request bytes.
func sendUpstream(ctx context.Context, clientReq *http.Request, provCfg *config.ProviderConfig, endpoint string, body []byte) (*http.Response, *FirstTokenDeadline, error) {
	timeout := provCfg.FirstTokenTimeout(0)
	client := provCfg.HTTPClient(0)
	resolvedURL := JoinBaseURLPath(provCfg.URL, endpoint)

	httpReq, err := buildUpstreamRequestAtURL(ctx, clientReq, provCfg, resolvedURL, body)
	if err != nil {
		return nil, nil, err
	}
	resp, deadline, err := DoWithFirstTokenTimeout(client, httpReq, timeout)
	if err == nil && resp.StatusCode == http.StatusNotFound && !baseURLHasPathSuffix(provCfg.URL, "/v1") {
		// Some API gateways expose endpoints under /v1 even when the base URL
		// omits it (e.g. Anthropic-compatible providers configured without /v1).
		// Retry once with /v1 prefix before giving up.
		resp.Body.Close()
		altReq, altErr := buildUpstreamRequestAtURL(ctx, clientReq, provCfg, prependPathPrefix(resolvedURL, "/v1"), body)
		if altErr == nil {
			altResp, altDeadline, altErr := DoWithFirstTokenTimeout(client, altReq, timeout)
			if altErr == nil {
				return altResp, altDeadline, nil
			}
			var altUpErr *sel.UpstreamError
			if errors.As(altErr, &altUpErr) {
				return nil, altDeadline, altErr
			}
			return nil, altDeadline, fmt.Errorf("send request: %w", altErr)
		}
	}
	if err == nil {
		return resp, deadline, nil
	}
	var upErr *sel.UpstreamError
	if errors.As(err, &upErr) {
		return nil, deadline, err
	}
	return nil, deadline, fmt.Errorf("send request: %w", err)
}

func prependPathPrefix(targetURL, prefix string) string {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		if strings.HasPrefix(targetURL, "/") {
			return strings.TrimRight(prefix, "/") + targetURL
		}
		return strings.TrimRight(prefix, "/") + "/" + targetURL
	}
	parsed.Path = strings.TrimRight(prefix, "/") + parsed.Path
	return parsed.String()
}

func baseURLHasPathSuffix(baseURL, suffix string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.HasSuffix(strings.TrimRight(baseURL, "/"), suffix)
	}
	return strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), suffix)
}

func readHTTPResponseBodyWithDeadline(resp *http.Response, deadline *FirstTokenDeadline, timeout time.Duration, context string) ([]byte, time.Duration, error) {
	respBody, err := readHTTPResponseBody(resp)
	latency := deadline.Latency()
	if err != nil {
		return respBody, latency, wrapFirstTokenReadError(deadline, timeout, context, err)
	}
	return respBody, latency, nil
}

// ReadRawResponseBodyWithFirstTokenDeadline reads and closes a response body while preserving
// the first-token timeout error shape used by upstream request handling.
func ReadRawResponseBodyWithFirstTokenDeadline(resp *http.Response, deadline *FirstTokenDeadline, timeout time.Duration) ([]byte, time.Duration, error) {
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	latency := deadline.Latency()
	if err != nil {
		return respBody, latency, wrapFirstTokenReadError(deadline, timeout, "read response body", err)
	}
	return respBody, latency, nil
}

func readAllAndCloseWithDeadline(reader io.ReadCloser, deadline *FirstTokenDeadline, timeout time.Duration, context string) ([]byte, time.Duration, error) {
	respBody, err := readAllAndClose(reader)
	latency := deadline.Latency()
	if err != nil {
		return respBody, latency, wrapFirstTokenReadError(deadline, timeout, context, err)
	}
	return respBody, latency, nil
}

func wrapFirstTokenReadError(deadline *FirstTokenDeadline, timeout time.Duration, context string, err error) error {
	if timeoutErr := deadline.TimeoutError(timeout); timeoutErr != nil {
		return fmt.Errorf("read first token: %w", timeoutErr)
	}
	return fmt.Errorf("%s: %w", context, err)
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
