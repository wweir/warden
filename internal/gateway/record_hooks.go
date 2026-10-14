package gateway

import (
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	"github.com/wweir/warden/pkg/toolhook"
)

// recordWithAsyncHooks emits the request record immediately with the blocking
// hook verdicts, then emits an updated record once async hook verdicts arrive.
// recordTokens is only passed to the first emit so token counters are counted
// once instead of once per emit.
func (g *Gateway) recordWithAsyncHooks(
	runAsync asyncHookFn,
	blockVerdicts []toolhook.HookVerdict,
	emit func(verdicts []toolhook.HookVerdict, recordTokens func(telemetrypkg.Labels, tokenusagepkg.Observation, int64)),
) {
	emit(blockVerdicts, g.RecordTokenMetrics)
	runAsync(func(asyncVerdicts []toolhook.HookVerdict) {
		if len(asyncVerdicts) == 0 {
			return
		}
		emit(appendVerdicts(blockVerdicts, asyncVerdicts), nil)
	})
}

// appendVerdicts concatenates block and async verdicts without aliasing either
// input slice.
func appendVerdicts(block, async []toolhook.HookVerdict) []toolhook.HookVerdict {
	combined := make([]toolhook.HookVerdict, 0, len(block)+len(async))
	combined = append(combined, block...)
	return append(combined, async...)
}
