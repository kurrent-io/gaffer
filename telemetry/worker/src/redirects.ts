// The public telemetry notice now lives in the docs site under
// /telemetry. These routes used to serve the rendered notice HTML; they
// now 301 to the docs hub so links baked into shipped gaffer binaries
// keep resolving - the first-run disclosure footer points at the root,
// and `/cli` is referenced elsewhere. The ingest endpoint (/v1/ingest)
// is unaffected. The docs origin is injected per environment (DOCS_BASE_URL)
// so the staging worker redirects to staging docs, not production.
const routes: Record<string, string> = {
	"/": "/telemetry/",
	"/cli": "/telemetry/cli/",
	"/vscode": "/telemetry/vs-code/",
};

/**
 * Returns a 301 redirect Response for a former notice route, or null if
 * the path was never a notice route. Trailing slashes on non-root paths
 * are stripped so `/cli` and `/cli/` resolve to the same target.
 */
export function handleNoticeRedirect(pathname: string, docsBaseUrl: string): Response | null {
	const normalized = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
	const path = routes[normalized];
	if (path === undefined) return null;
	return new Response(null, { status: 301, headers: { location: new URL(path, docsBaseUrl).toString() } });
}
