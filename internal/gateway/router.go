package gateway

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	httpmwpkg "github.com/wweir/warden/internal/gateway/httpmw"
	loggingpkg "github.com/wweir/warden/internal/gateway/logging"
	snapshotpkg "github.com/wweir/warden/internal/gateway/snapshot"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
)

type routeBinding struct {
	prefix string
	route  *config.RouteConfig
}

type routeHandler func(http.ResponseWriter, *http.Request, *config.RouteConfig)

// NewGateway creates a new Gateway instance with routes registered once.
func NewGateway(cfg *config.ConfigStruct, configPath, configHash string) *Gateway {
	var err error
	defer func() { deferlog.DebugError(err, "create new gateway") }()

	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		cfg:            cfg,
		configPath:     configPath,
		configHash:     configHash,
		selector:       sel.NewSelector(cfg),
		routes:         compileRouteBindings(cfg.Route),
		broadcaster:    reqlog.NewBroadcaster(),
		dashboardStore: telemetrypkg.NewDashboardMetricsStore(dashboardMetricsSampleInterval, dashboardMetricsHistoryLimit),
		outputRates:    telemetrypkg.NewOutputRateTracker(dashboardMetricsSampleInterval),
		ctx:            ctx,
		cancel:         cancel,
	}
	g.admin = g.adminHandler()

	go g.selector.RefreshModels(cfg)
	g.dashboardStore.Start(ctx, func() telemetrypkg.DashboardCounterSample {
		return snapshotpkg.CollectDashboardCounters(g.outputRates)
	})
	g.logger = loggingpkg.NewLogger(cfg.Log)
	g.handler = g.buildHTTPHandler()
	return g
}

func (g *Gateway) buildHTTPHandler() http.Handler {
	router := g.newRouter()
	g.registerAdminRoutes(router)
	g.RegisterMetricsRoutes(router)
	g.registerRouteBindings(router)
	router.NotFound = g.notFoundHandler()

	return httpmwpkg.Chain(
		&httpmwpkg.Recovery{},
		&httpmwpkg.CORS{},
		&httpmwpkg.APIKeyAuth{Cfg: g.cfg},
		&PromMiddleware{gateway: g},
	).Process(router)
}

func shouldRegisterOpenAIEndpoint(route *config.RouteConfig, serviceProtocol string) bool {
	switch serviceProtocol {
	case config.RouteProtocolChat:
		return route.ConfiguredProtocol() == config.RouteProtocolChat
	case config.RouteProtocolResponsesStateless:
		return config.IsResponsesRouteProtocol(route.ConfiguredProtocol())
	default:
		return false
	}
}

func (g *Gateway) newRouter() *httprouter.Router {
	router := httprouter.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	router.HandleOPTIONS = false
	router.HandleMethodNotAllowed = false
	return router
}

func (g *Gateway) registerAdminRoutes(router *httprouter.Router) {
	if g.cfg.AdminPassword != "" {
		g.adminHandler().RegisterRoutes(router)
	}
}

func (g *Gateway) registerRouteBindings(router *httprouter.Router) {
	for _, binding := range g.routes {
		g.registerRoute(router, binding)
	}
}

func (g *Gateway) registerRoute(router *httprouter.Router, binding routeBinding) {
	router.Handle(http.MethodGet, binding.prefix+"/models", g.bindRouteHandler(binding.route, g.handleModels))

	if shouldRegisterOpenAIEndpoint(binding.route, config.RouteProtocolChat) {
		router.Handle(http.MethodPost, binding.prefix+"/chat/completions", g.bindRouteHandler(binding.route, g.handleChatCompletion))
	}
	if shouldRegisterOpenAIEndpoint(binding.route, config.RouteProtocolResponsesStateless) {
		router.Handle(http.MethodPost, binding.prefix+"/responses", g.bindRouteHandler(binding.route, g.handleResponses))
	}
	if binding.route.ConfiguredProtocol() == config.RouteProtocolAnthropic {
		router.Handle(http.MethodPost, binding.prefix+"/messages", g.bindRouteHandler(binding.route, g.handleAnthropicMessages))
	}
}

func (g *Gateway) bindRouteHandler(route *config.RouteConfig, handler routeHandler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		handler(w, r, route)
	}
}

func (g *Gateway) notFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && g.cfg.AdminPassword != "" {
			http.Redirect(w, r, "/_admin/", http.StatusFound)
			return
		}

		binding, ok := g.matchProxyRoute(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		g.handleProxy(w, trimRoutePrefix(r, binding.prefix), binding.route)
	})
}

func (g *Gateway) matchProxyRoute(path string) (routeBinding, bool) {
	for _, binding := range g.routes {
		if strings.HasPrefix(path, binding.prefix+"/") {
			return binding, true
		}
	}
	return routeBinding{}, false
}

func compileRouteBindings(routes map[string]*config.RouteConfig) []routeBinding {
	bindings := make([]routeBinding, 0, len(routes))
	for prefix, route := range routes {
		bindings = append(bindings, routeBinding{prefix: prefix, route: route})
	}
	sort.Slice(bindings, func(i, j int) bool {
		if len(bindings[i].prefix) != len(bindings[j].prefix) {
			return len(bindings[i].prefix) > len(bindings[j].prefix)
		}
		return bindings[i].prefix < bindings[j].prefix
	})
	return bindings
}

func trimRoutePrefix(r *http.Request, prefix string) *http.Request {
	cloned := r.Clone(r.Context())
	urlCopy := *r.URL
	urlCopy.Path = strings.TrimPrefix(r.URL.Path, prefix)
	cloned.URL = &urlCopy
	return cloned
}
