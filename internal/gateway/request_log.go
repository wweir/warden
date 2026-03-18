package gateway

import (
	"net/http"
	"time"

	"github.com/wweir/warden/internal/reqlog"
)

type streamLogAssembler func(respBody []byte) (assembled []byte, fallback []byte, err error)

type inferenceLogParams struct {
	StartTime  time.Time
	RequestID  string
	Route      string
	Endpoint   string
	Model      string
	Stream     bool
	Provider   string
	UserAgent  string
	Request    []byte
	Failovers  []reqlog.Failover
	MetricTags requestMetricLabels
}

func newInferenceLogParams(r *http.Request, startTime time.Time, requestID, route, endpoint, model string, stream bool, requestBody []byte, failovers []reqlog.Failover, labels requestMetricLabels, provider string) inferenceLogParams {
	return inferenceLogParams{
		StartTime:  startTime,
		RequestID:  requestID,
		Route:      route,
		Endpoint:   endpoint,
		Model:      model,
		Stream:     stream,
		Provider:   provider,
		UserAgent:  r.UserAgent(),
		Request:    requestBody,
		Failovers:  failovers,
		MetricTags: labels,
	}
}

func (g *Gateway) recordInferenceLog(params inferenceLogParams, respBody []byte, errMsg string, assembleStream streamLogAssembler) {
	rec := reqlog.Record{
		Timestamp:   params.StartTime,
		RequestID:   params.RequestID,
		Route:       params.Route,
		Endpoint:    params.Endpoint,
		Model:       params.Model,
		Stream:      params.Stream,
		Provider:    params.Provider,
		UserAgent:   params.UserAgent,
		DurationMs:  time.Since(params.StartTime).Milliseconds(),
		Error:       errMsg,
		Fingerprint: reqlog.BuildFingerprint(params.Request),
		Request:     params.Request,
		Response:    respBody,
		Failovers:   params.Failovers,
	}

	if len(respBody) > 0 && errMsg == "" {
		if params.Stream && assembleStream != nil {
			assembled, fallback, err := assembleStream(respBody)
			if err == nil {
				rec.Response = assembled
				g.RecordTokenMetrics(params.MetricTags, ExtractTokenUsage(assembled), rec.DurationMs)
			} else {
				if len(fallback) == 0 {
					fallback = respBody
				}
				rec.Response = marshalRawStreamForLog(fallback)
			}
		} else {
			g.RecordTokenMetrics(params.MetricTags, ExtractTokenUsage(respBody), rec.DurationMs)
		}
	}

	g.recordAndBroadcast(rec)
}

func (g *Gateway) publishPendingInferenceLog(params inferenceLogParams) {
	g.broadcaster.Publish(reqlog.Record{
		Timestamp:   params.StartTime,
		RequestID:   params.RequestID,
		Route:       params.Route,
		Endpoint:    params.Endpoint,
		Model:       params.Model,
		Stream:      params.Stream,
		Pending:     true,
		Provider:    params.Provider,
		UserAgent:   params.UserAgent,
		Fingerprint: reqlog.BuildFingerprint(params.Request),
		Request:     params.Request,
		Failovers:   params.Failovers,
	})
}
