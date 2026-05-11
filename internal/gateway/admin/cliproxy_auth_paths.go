package admin

// Path tables for cliproxy auth usage extraction.
//
// summarizeCLIProxyAuthUsage scans a sanitized auth JSON looking for usage and
// reset-time fields under three top-level prefixes (usage / quota / limits).
// runtimeUsageFromSuppressReason scans a smaller error-body envelope and only
// inspects flat root-level keys. The candidate field-name lists are shared, so
// they live here and the path-generation helpers do the cartesian join.

// usageRootPrefixes is the list of top-level objects scanned for usage
// metrics inside an auth file.
var usageRootPrefixes = []string{"usage", "quota", "limits"}

// Base name variants used when scanning for metric values. Names that imply a
// "limit" or "quota" field are appended to the base list when scanning the
// value; reset-time scans use only the base names because reset_at /
// reset_after never hang off a *_limit / *_quota subkey.
var (
	usageMetric5HBaseVariants      = []string{"5h", "5_hour", "five_hour", "fiveHour"}
	usageMetric5HExtraVariants     = []string{"five_hour_limit", "five_hour_quota"}
	usageMetricWeeklyBaseVariants  = []string{"weekly", "week", "7d", "seven_day", "sevenDay"}
	usageMetricWeeklyExtraVariants = []string{"weekly_limit", "weekly_quota"}
)

// Pre-computed gjson path slices used by summarizeCLIProxyAuthUsage.
var (
	usagePaths5H          = buildUsageMetricPaths(usageMetric5HBaseVariants, usageMetric5HExtraVariants)
	usagePaths5HReset     = buildUsageResetPaths(usageMetric5HBaseVariants)
	usagePathsWeekly      = buildUsageMetricPaths(usageMetricWeeklyBaseVariants, usageMetricWeeklyExtraVariants)
	usagePathsWeeklyReset = buildUsageResetPaths(usageMetricWeeklyBaseVariants)
)

// buildUsageMetricPaths returns the full path candidate list for a usage
// metric: each prefix x (base ++ extra) variants in the historical order.
func buildUsageMetricPaths(base, extra []string) []string {
	variants := make([]string, 0, len(base)+len(extra))
	variants = append(variants, base...)
	variants = append(variants, extra...)
	out := make([]string, 0, len(usageRootPrefixes)*len(variants))
	for _, prefix := range usageRootPrefixes {
		for _, v := range variants {
			out = append(out, prefix+"."+v)
		}
	}
	return out
}

// buildUsageResetPaths returns the reset-time path candidates. The historical
// order is: usage prefix with reset_at, usage prefix with reset_after, quota
// prefix with reset_at, limits prefix with reset_at. quota and limits never
// included reset_after.
func buildUsageResetPaths(base []string) []string {
	out := make([]string, 0, len(base)*4)
	for _, v := range base {
		out = append(out, "usage."+v+".reset_at")
	}
	for _, v := range base {
		out = append(out, "usage."+v+".reset_after")
	}
	for _, v := range base {
		out = append(out, "quota."+v+".reset_at")
	}
	for _, v := range base {
		out = append(out, "limits."+v+".reset_at")
	}
	return out
}

// Path tables for runtime error-body extraction. The runtime envelope is a
// single error object so the paths are flat (no usage./quota./limits.
// prefixes) and use a smaller variant set than the auth-file table.
var (
	runtimeWeeklyPaths      = []string{"weekly", "week", "7d", "weekly_limit", "weekly_quota"}
	runtimeWeeklyResetPaths = []string{"weekly.reset_at", "week.reset_at", "7d.reset_at", "weekly_reset_at"}
)
