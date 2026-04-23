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
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminStatus(w, r, nil)
	})
	mux.HandleFunc("GET /api/config/source", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminConfigSource(w, r, nil)
	})
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminConfigGet(w, r, nil)
	})
	mux.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAdminConfigPut(w, r, nil)
	})
	mux.HandleFunc("POST /api/config/validate", func(w http.ResponseWriter, r *http.Request) {
		h.HandleConfigValidate(w, r, nil)
	})
	mux.HandleFunc("GET /api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		h.HandleLogStream(w, r, nil)
	})
	mux.HandleFunc("GET /api/tool-hooks/suggestions", func(w http.ResponseWriter, r *http.Request) {
		h.HandleToolHookSuggestions(w, r, nil)
	})
	mux.HandleFunc("POST /api/restart", func(w http.ResponseWriter, r *http.Request) {
		h.HandleRestart(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/health", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderHealth(w, r, nil)
	})
	mux.HandleFunc("GET /api/providers/form-meta", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderFormMeta(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/protocols/detect", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderProtocolDetect(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/protocols/probe", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderModelProtocolProbe(w, r, nil)
	})
	mux.HandleFunc("GET /api/providers/detail", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderDetail(w, r, nil)
	})
	mux.HandleFunc("POST /api/providers/suppress", func(w http.ResponseWriter, r *http.Request) {
		h.HandleProviderSuppress(w, r, nil)
	})
	mux.HandleFunc("GET /api/routes/detail", func(w http.ResponseWriter, r *http.Request) {
		h.HandleRouteDetail(w, r, nil)
	})
	mux.HandleFunc("GET /api/metrics/stream", func(w http.ResponseWriter, r *http.Request) {
		h.HandleMetricsStream(w, r, nil)
	})
	mux.HandleFunc("GET /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAPIKeysList(w, r, nil)
	})
	mux.HandleFunc("POST /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAPIKeysCreate(w, r, nil)
	})
	mux.HandleFunc("DELETE /api/apikeys", func(w http.ResponseWriter, r *http.Request) {
		h.HandleAPIKeysDelete(w, r, nil)
	})
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
