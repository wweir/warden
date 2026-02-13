package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"warden/config"
	"warden/pkg/openai"
)

// Gateway 是 AI Gateway 的核心组件
type Gateway struct {
	cfg   *config.ConfigStruct
	chain *ChainMiddleware
}

// NewGateway 创建新的 Gateway 实例
func NewGateway(cfg *config.ConfigStruct) *Gateway {
	// 配置中间件链
	chain := Chain(
		&LoggingMiddleware{},
		&RecoveryMiddleware{},
		&CORS{},
	)

	return &Gateway{
		cfg:   cfg,
		chain: chain.(*ChainMiddleware),
	}
}

// ServeHTTP 实现 http.Handler 接口
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var router http.Handler = httprouter.New()

	for prefix, route := range g.cfg.Route {
		router = http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.handleRoute(w, r, route)
		}))

		http.Handle(prefix+"/", router)
	}

	g.chain.Process(router).ServeHTTP(w, r)
}

// handleRoute 处理特定路由的请求
func (g *Gateway) handleRoute(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	switch r.URL.Path {
	case "/v1/chat/completions":
		g.handleChatCompletion(w, r, route)
	default:
		http.NotFound(w, r)
	}
}

// handleChatCompletion 处理 Chat Completion 请求
func (g *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 1. 注入 MCP 工具
	var toolsInjected bool
	if len(route.Tools) > 0 {
		for _, toolName := range route.Tools {
			if route.EnabledTools[toolName] {
				// 从 MCP 配置中获取工具信息并注入
				req.Tools = append(req.Tools, openai.Tool{
					Type: "function",
					Function: openai.Function{
						Name:        toolName,
						Description: "MCP tool function",
					},
				})
				toolsInjected = true
			}
		}
	}

	slog.Debug("Request received", "route", route.Prefix, "model", req.Model, "tools_injected", toolsInjected)

	// 2. 发送到上游
	firstBaseURL := route.BaseURLs[0] // 目前直接取第一个
	buCfg, ok := g.cfg.BaseURL[firstBaseURL]
	if !ok {
		http.Error(w, "BaseURL not found", http.StatusInternalServerError)
		return
	}

	g.forwardRequest(w, r, buCfg, req)
}

// forwardRequest 转发请求到上游，并处理协议转换
func (g *Gateway) forwardRequest(w http.ResponseWriter, r *http.Request, buCfg *config.BaseURLConfig, req openai.ChatCompletionRequest) {
	// 获取适配器
	adapter, err := NewAdapter(buCfg.Protocol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 协议转换
	convertedReq, err := adapter.ConvertRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Debug("Protocol converted", "protocol", buCfg.Protocol, "model", req.Model)

	// 发送请求到上游
	upstreamResp, err := sendUpstreamRequest(buCfg, convertedReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 转换响应
	openaiResp, err := adapter.ConvertResponse(upstreamResp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回响应
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
