import { cliHtml, introHtml, vscodeHtml } from "./notice.gen";

// Locked-down CSP for a script-free static page. `default-src 'none'`
// denies everything, then we re-allow only what the template uses:
// inline <style>, same-origin fonts (/fonts/*), same-origin favicons
// (/favicons/*, served as <link rel="icon"> which CSP treats as img).
// No script, no connect, no frame, no form, no base. Defence-in-depth -
// if a future edit ever introduces a <script> tag, the browser blocks
// it instead of silently running it.
const CSP = [
	"default-src 'none'",
	"script-src 'none'",
	"style-src 'self' 'unsafe-inline'",
	"font-src 'self'",
	"img-src 'self'",
	"base-uri 'none'",
	"form-action 'none'",
	"frame-ancestors 'none'",
].join("; ");

const HEADERS = {
	"content-type": "text/html; charset=utf-8",
	"content-security-policy": CSP,
	// Belt-and-braces hardening for the static pages. CSP already
	// blocks script execution; these stop browsers from sniffing past
	// the declared content-type and from leaking referrer info on the
	// outbound links in the notice bodies.
	"x-content-type-options": "nosniff",
	"referrer-policy": "no-referrer",
	// The notices are static between deploys; cache aggressively but
	// allow revalidation so a redeploy invalidates browsers' caches
	// when content actually changes.
	"cache-control": "public, max-age=300, must-revalidate",
};

const pages: Record<string, string> = {
	"/": introHtml,
	"/cli": cliHtml,
	"/vscode": vscodeHtml,
};

/**
 * Returns the notice Response for `pathname`, or null if the path is
 * not a notice route. Trailing slashes on non-root paths are stripped
 * so `/cli` and `/cli/` resolve to the same page. The caller is
 * responsible for HEAD vs GET and method-not-allowed handling.
 */
export function handleNotice(pathname: string): Response | null {
	const normalized = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
	const html = pages[normalized];
	if (html === undefined) return null;
	return new Response(html, { headers: HEADERS });
}
