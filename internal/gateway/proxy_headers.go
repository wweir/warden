package gateway

import (
	"net"
	"net/http"
	"net/textproto"
	"strings"
)

var hopByHopRequestHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

var upstreamOverrideHeaders = []string{
	"Authorization",
	"Api-Key",
	"X-Api-Key",
	"Cookie",
	"Content-Length",
	"Forwarded",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Real-Ip",
}

func buildProxyRequestHeaders(in *http.Request, inferenceEndpoint bool) http.Header {
	headers := buildForwardedRequestHeaders(in)

	if acceptedEncoding := negotiateProxyAcceptEncoding(in.Header.Get("Accept-Encoding"), inferenceEndpoint); acceptedEncoding != "" {
		headers.Set("Accept-Encoding", acceptedEncoding)
	} else {
		headers.Del("Accept-Encoding")
	}
	return headers
}

func buildForwardedRequestHeaders(in *http.Request) http.Header {
	headers := in.Header.Clone()
	sanitizeProxyRequestHeaders(headers)
	setForwardedHeaders(headers, in)
	return headers
}

func sanitizeProxyRequestHeaders(headers http.Header) {
	removeConnectionSpecificHeaders(headers)

	for _, headerName := range hopByHopRequestHeaders {
		headers.Del(headerName)
	}
	for _, headerName := range upstreamOverrideHeaders {
		headers.Del(headerName)
	}
}

func removeConnectionSpecificHeaders(headers http.Header) {
	for _, connectionHeader := range []string{"Connection", "Proxy-Connection"} {
		for _, rawValue := range headers.Values(connectionHeader) {
			for _, token := range strings.Split(rawValue, ",") {
				headerName := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(token))
				if headerName == "" {
					continue
				}
				headers.Del(headerName)
			}
		}
	}
}

func setForwardedHeaders(headers http.Header, in *http.Request) {
	clientIP := clientIPFromRemoteAddr(in.RemoteAddr)
	if clientIP != "" {
		headers.Set("X-Forwarded-For", clientIP)
		headers.Set("X-Real-Ip", clientIP)
	} else {
		headers.Del("X-Forwarded-For")
		headers.Del("X-Real-Ip")
	}

	headers.Set("X-Forwarded-Proto", requestScheme(in))
	if in.Host != "" {
		headers.Set("X-Forwarded-Host", in.Host)
	} else {
		headers.Del("X-Forwarded-Host")
	}
}

func requestScheme(in *http.Request) string {
	if in.TLS != nil {
		return "https"
	}
	if in.URL != nil && in.URL.Scheme != "" {
		return in.URL.Scheme
	}
	return "http"
}

func clientIPFromRemoteAddr(remoteAddr string) string {
	trimmed := strings.TrimSpace(remoteAddr)
	if trimmed == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(trimmed)
	if err == nil {
		return host
	}
	return trimmed
}
