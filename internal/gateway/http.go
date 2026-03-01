package gateway

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/anthropic"
)

// sendRequest sends a raw request body to the upstream endpoint and returns the raw response body.
func sendRequest(provCfg *config.ProviderConfig, endpoint string, body []byte) ([]byte, error) {
	httpReq, err := http.NewRequest(http.MethodPost, provCfg.URL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	selector.SetAuthHeaders(httpReq.Header, provCfg)

	client := provCfg.HTTPClient(0)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	reader := resp.Body
	// Manually decompress gzip when the transport did not handle it
	// (e.g. upstream returns gzip without proper Content-Encoding header).
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			return nil, fmt.Errorf("create gzip reader: %w", gzErr)
		}
		defer gr.Close()
		reader = gr
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
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
		return nil, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	// detect HTML body on 200 (misconfigured proxy returning HTML instead of JSON)
	if trimmed := strings.TrimSpace(string(respBody)); len(trimmed) > 0 && (trimmed[0] == '<' || strings.HasPrefix(trimmed, "<!DOCTYPE")) {
		return nil, &selector.UpstreamError{Code: resp.StatusCode, Body: trimmed}
	}

	// detect error in HTTP 200 response body (some APIs return errors with 200 status)
	if errType, _ := selector.ParseErrorBody(string(respBody)); errType != "" && selector.IsRetryableByBody(string(respBody)) {
		return nil, &selector.UpstreamError{Code: resp.StatusCode, Body: string(respBody)}
	}

	return respBody, nil
}

// pipeRawStream sends a raw request body upstream and returns the response bytes
// after writing them to the client.
func pipeRawStream(w http.ResponseWriter, provCfg *config.ProviderConfig, endpoint string, body []byte) ([]byte, error) {
	rawBody, err := sendRequest(provCfg, endpoint, body)
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
