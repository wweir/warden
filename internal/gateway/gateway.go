package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/wweir/warden/config"
	adminpkg "github.com/wweir/warden/internal/gateway/admin"
	proxypkg "github.com/wweir/warden/internal/gateway/proxy"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/toolhook"
)

// Gateway is the core AI Gateway component.
type Gateway struct {
	cfg        *config.ConfigStruct
	configPath string
	configHash string
	selector   *sel.Selector
	routes     []routeBinding

	logger                reqlog.Logger
	broadcaster           *reqlog.Broadcaster
	dashboardStore        *telemetrypkg.DashboardMetricsStore
	outputRates           *telemetrypkg.OutputRateTracker
	internalHookAuthToken string
	admin                 *adminpkg.Handler
	proxy                 *proxypkg.Handler
	handler               http.Handler
	reloadFn              func() error
	ctx                   context.Context
	cancel                context.CancelFunc
}

const (
	dashboardMetricsSampleInterval = 2 * time.Second
	dashboardMetricsHistoryLimit   = 180
	dashboardOutputRateStaleAfter  = 8 * time.Second
)

// SetReloadFn sets the function called to hot-reload the gateway.
func (g *Gateway) SetReloadFn(fn func() error) {
	g.reloadFn = fn
	g.adminHandler().SetReloadFn(fn)
}

func (g *Gateway) adminHandler() *adminpkg.Handler {
	if g.admin != nil {
		return g.admin
	}
	g.admin = adminpkg.NewHandler(adminpkg.Deps{
		Cfg:         g.cfg,
		ConfigPath:  &g.configPath,
		ConfigHash:  &g.configHash,
		Selector:    g.selector,
		Broadcaster: g.broadcaster,
		ReloadFn:    g.reloadFn,
		CollectMetricsData: func() map[string]any {
			return telemetrypkg.CollectMetricsData(g.selector.ProviderStatuses(), g.outputRates, g.dashboardStore)
		},
		ListAPIKeys: func() []map[string]any {
			return telemetrypkg.ListAPIKeysPayload(g.cfg.Route)
		},
	})
	return g.admin
}

func (g *Gateway) proxyHandler() *proxypkg.Handler {
	if g.proxy != nil {
		return g.proxy
	}
	g.proxy = proxypkg.NewHandler(proxypkg.Deps{
		Cfg:                g.cfg,
		Selector:           g.selector,
		ApplyMetricHeaders: applyInferenceMetricHeaders,
		LogRequest:         logRequest,
		RecordFailover:     g.RecordFailoverMetric,
		RecordTTFT:         g.RecordTTFTMetric,
		RecordTokenMetrics: g.RecordTokenMetrics,
		RecordAndBroadcast: g.recordAndBroadcast,
	})
	return g.proxy
}

// ServeHTTP implements http.Handler.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.handler.ServeHTTP(w, r)
}

// Close shuts down the gateway runtime and the request logger.
func (g *Gateway) Close() {
	g.cancel()
	if g.logger != nil {
		g.logger.Close()
	}
}

// Broadcaster returns the request log broadcaster for admin subscriptions.
func (g *Gateway) Broadcaster() *reqlog.Broadcaster {
	return g.broadcaster
}

func (g *Gateway) hookGatewayTarget() toolhook.GatewayTarget {
	return toolhook.GatewayTarget{
		Addr:              g.cfg.Addr,
		InternalAuthToken: g.internalHookAuthToken,
	}
}

func mustNewInternalHookAuthToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("generate internal hook auth token: %v", err))
	}
	return hex.EncodeToString(raw[:])
}

// recordAndBroadcast logs a record to file (if enabled) and publishes to SSE subscribers.
func (g *Gateway) recordAndBroadcast(r reqlog.Record) {
	r.Sanitize()
	if g.logger != nil {
		g.logger.Log(r)
	}
	g.broadcaster.Publish(r)
}

func (g *Gateway) handleProxy(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	g.proxyHandler().Handle(w, withRouteRequestContext(r, route), route)
}

func (g *Gateway) handleModels(w http.ResponseWriter, _ *http.Request, route *config.RouteConfig) {
	g.proxyHandler().HandleModels(w, route)
}
