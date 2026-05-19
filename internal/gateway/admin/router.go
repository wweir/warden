package admin

import (
	"bytes"
	"crypto/subtle"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/julienschmidt/httprouter"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/web"
)

// RegisterRoutes registers all /_admin/ routes with Basic Auth.
// Uses an internal http.ServeMux for sub-routing to avoid httprouter
// wildcard conflicts between /_admin/*filepath and /_admin/api/* routes.
func (h *Handler) RegisterRoutes(router *httprouter.Router) {
	auth := h.basicAuth

	adminFS, err := fs.Sub(web.AdminFS, "admin/dist")
	if err != nil {
		slog.Warn("Failed to load admin frontend", "error", err)
		return
	}
	fileServer := http.FileServer(http.FS(adminFS))
	readFS := adminFS.(fs.ReadFileFS)

	serveFSFile := func(w http.ResponseWriter, r *http.Request, name string) {
		brData, brErr := readFS.ReadFile(name + ".br")
		if brErr == nil {
			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
			w.Header().Set("Vary", "Accept-Encoding")
			if upstreampkg.SelectAcceptedEncoding(r.Header.Get("Accept-Encoding"), []string{"br"}) == "br" {
				w.Header().Set("Content-Encoding", "br")
				_, _ = w.Write(brData)
				return
			}
			if _, err := io.Copy(w, brotli.NewReader(bytes.NewReader(brData))); err != nil {
				slog.Error("brotli decompress failed", "file", name, "error", err)
			}
			return
		}
		r.URL.Path = "/" + name
		fileServer.ServeHTTP(w, r)
	}

	mux := http.NewServeMux()
	for _, route := range h.apiRoutes() {
		mux.HandleFunc(route.method+" "+route.path, func(w http.ResponseWriter, r *http.Request) {
			route.handle(w, r, nil)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fp := strings.TrimPrefix(r.URL.Path, "/")
		if fp != "" {
			if _, err := readFS.ReadFile(fp + ".br"); err == nil {
				serveFSFile(w, r, fp)
				return
			}
			if _, err := readFS.ReadFile(fp); err == nil {
				serveFSFile(w, r, fp)
				return
			}
		}
		serveFSFile(w, r, "index.html")
	})

	adminHandler := auth(func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/_admin")
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		mux.ServeHTTP(w, r)
	})

	router.Handle(http.MethodGet, "/_admin", adminHandler)
	router.Handle(http.MethodGet, "/_admin/*filepath", adminHandler)
	router.Handle(http.MethodPut, "/_admin/*filepath", adminHandler)
	router.Handle(http.MethodPost, "/_admin/*filepath", adminHandler)
	router.Handle(http.MethodDelete, "/_admin/*filepath", adminHandler)
}

func (h *Handler) basicAuth(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || subtle.ConstantTimeCompare([]byte(pass), []byte(h.cfg.AdminPassword.Value())) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Warden Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, ps)
	}
}

type adminAPIRoute struct {
	method string
	path   string
	handle func(http.ResponseWriter, *http.Request, httprouter.Params)
}

func (h *Handler) apiRoutes() []adminAPIRoute {
	return []adminAPIRoute{
		{http.MethodGet, "/api/status", h.HandleAdminStatus},
		{http.MethodGet, "/api/config/source", h.HandleAdminConfigSource},
		{http.MethodGet, "/api/config", h.HandleAdminConfigGet},
		{http.MethodPut, "/api/config", h.HandleAdminConfigPut},
		{http.MethodPost, "/api/config/validate", h.HandleConfigValidate},
		{http.MethodGet, "/api/logs/stream", h.HandleLogStream},
		{http.MethodGet, "/api/tool-hooks/suggestions", h.HandleToolHookSuggestions},
		{http.MethodPost, "/api/restart", h.HandleRestart},
		{http.MethodPost, "/api/providers/health", h.HandleProviderHealth},
		{http.MethodPost, "/api/providers/probe-access", h.HandleProviderProbeAccess},
		{http.MethodGet, "/api/providers/form-meta", h.HandleProviderFormMeta},
		{http.MethodGet, "/api/cliproxy/auth-files", h.HandleCLIProxyAuthFilesList},
		{http.MethodPost, "/api/cliproxy/auth-files", h.HandleCLIProxyAuthFileCreate},
		{http.MethodDelete, "/api/cliproxy/auth-files", h.HandleCLIProxyAuthFileDelete},
		{http.MethodPost, "/api/cliproxy/auth-files/verify", h.HandleCLIProxyAuthFileVerify},
		{http.MethodGet, "/api/cliproxy/auth-files/usage", h.HandleCLIProxyAuthFileUsage},
		{http.MethodPost, "/api/providers/protocols/detect", h.HandleProviderProtocolDetect},
		{http.MethodPost, "/api/providers/protocols/probe", h.HandleProviderModelProtocolProbe},
		{http.MethodGet, "/api/providers/detail", h.HandleProviderDetail},
		{http.MethodPost, "/api/providers/suppress", h.HandleProviderSuppress},
		{http.MethodGet, "/api/routes/detail", h.HandleRouteDetail},
		{http.MethodGet, "/api/metrics/stream", h.HandleMetricsStream},
		{http.MethodGet, "/api/apikeys", h.HandleAPIKeysList},
		{http.MethodPost, "/api/apikeys", h.HandleAPIKeysCreate},
		{http.MethodDelete, "/api/apikeys", h.HandleAPIKeysDelete},
	}
}
