// The public telemetry notice now lives in the docs site under
// /telemetry. These routes used to serve the rendered notice HTML; they
// now 301 to the docs hub so links baked into shipped gaffer binaries
// keep resolving - the first-run disclosure footer points at the root,
// and `/cli` is referenced elsewhere. The ingest endpoint (/v1/ingest)
// is unaffected.
const DOCS_HUB = "https://gaffer.kurrent.io/telemetry";

const redirects: Record<string, string> = {
	"/": `${DOCS_HUB}/`,
	"/cli": `${DOCS_HUB}/cli/`,
	"/vscode": `${DOCS_HUB}/vs-code/`,
};

/**
 * Returns a 301 redirect Response for a former notice route, or null if
 * the path was never a notice route. Trailing slashes on non-root paths
 * are stripped so `/cli` and `/cli/` resolve to the same target.
 */
export function handleNoticeRedirect(pathname: string): Response | null {
	const normalized = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
	const location = redirects[normalized];
	if (location === undefined) return null;
	return new Response(null, { status: 301, headers: { location } });
}
