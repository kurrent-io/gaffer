// PostHog web analytics for the docs site. The project token is a public
// ingest key, not a secret, so it lives in source. EU ingest host - change
// region only if the project is migrated.
//
// Page views go to the shared Kurrent web telemetry pool, which is separate
// from gaffer's product telemetry (what the CLI and VS Code extension send
// via the telemetry worker). See /telemetry for the product side.
export const POSTHOG = {
	token: "phc_DeHBgHGersY4LmDlADnPrsCPOAmMO7QFOH8f4DVEVmD",
	apiHost: "https://eu.i.posthog.com",
} as const;
