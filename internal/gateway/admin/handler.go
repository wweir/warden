package admin

import (
	"encoding/json"
	"sync"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
)

type Selector interface {
	ProviderStatuses() []sel.ProviderStatus
	ProviderDetail(name string) *sel.ProviderStatus
	ProviderModels(name string) []json.RawMessage
	ModelProtocolProbes(name string) []sel.ModelProtocolProbe
	SetManualSuppress(name string, suppress bool) bool
	SetDisplayProtocols(name string, protocols []string, probe *sel.ProtocolProbe) bool
	UpsertModelProtocolProbe(name string, probe sel.ModelProtocolProbe) bool
}

type Broadcaster interface {
	Subscribe() chan reqlog.Record
	Unsubscribe(ch chan reqlog.Record)
	Recent() []reqlog.Record
}

type Deps struct {
	Cfg                *config.ConfigStruct
	ConfigPath         *string
	ConfigHash         *string
	Selector           Selector
	Broadcaster        Broadcaster
	ReloadFn           func() error
	CollectMetricsData func() map[string]any
	ListAPIKeys        func() []map[string]any
}

type Handler struct {
	cfg                *config.ConfigStruct
	configPath         *string
	configHash         *string
	configMu           sync.Mutex
	selector           Selector
	broadcaster        Broadcaster
	reloadFn           func() error
	collectMetricsData func() map[string]any
	listAPIKeys        func() []map[string]any
}

func NewHandler(deps Deps) *Handler {
	return &Handler{
		cfg:                deps.Cfg,
		configPath:         deps.ConfigPath,
		configHash:         deps.ConfigHash,
		selector:           deps.Selector,
		broadcaster:        deps.Broadcaster,
		reloadFn:           deps.ReloadFn,
		collectMetricsData: deps.CollectMetricsData,
		listAPIKeys:        deps.ListAPIKeys,
	}
}

func (h *Handler) SetReloadFn(fn func() error) {
	h.reloadFn = fn
}

func (h *Handler) configPathValue() string {
	if h.configPath == nil {
		return ""
	}
	return *h.configPath
}

func (h *Handler) configHashValue() string {
	if h.configHash == nil {
		return ""
	}
	return *h.configHash
}

func (h *Handler) setConfigHash(value string) {
	if h.configHash != nil {
		*h.configHash = value
	}
}
