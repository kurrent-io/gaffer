// Bucket helpers for telemetry properties. Match the CLI's bucketing
// rounding policy so dashboards aggregate uniformly across surfaces.

import type { DurationBucket } from "@kurrent/gaffer-telemetry";

// Same buckets the CLI's RawDuration uses (cli/internal/telemetry/
// emit.go). Schema's #DurationBucket enum is the source of truth;
// keep this array in sync.
const DURATION_BUCKETS: readonly DurationBucket[] = [
	0, 10, 100, 1000, 10000, 60000, 600000,
] as const;

/**
 * Round milliseconds down to the largest DurationBucket value <= ms.
 * Negative inputs (e.g. a clock skew between performance.now samples)
 * collapse to 0.
 */
export function bucketDuration(ms: number): DurationBucket {
	if (!Number.isFinite(ms) || ms < 0) return 0;
	let bucket: DurationBucket = 0;
	for (const candidate of DURATION_BUCKETS) {
		if (ms >= candidate) bucket = candidate;
		else break;
	}
	return bucket;
}

/**
 * Truncate a gaffer-version string to a "major.minor" form for the
 * `cli_version` envelope property. Anything we can't parse (empty,
 * non-semver-prefix, pre-release-only) collapses to "unknown" so the
 * worker still gets a categorical value.
 */
export function bucketCliVersion(version: string): string {
	const match = /^v?(\d+)\.(\d+)/.exec(version.trim());
	if (match === null) return "unknown";
	return `${match[1]}.${match[2]}`;
}
