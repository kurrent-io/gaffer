import { noticeHtml } from "./notice.gen";

export function handleNotice(): Response {
	return new Response(noticeHtml, {
		headers: {
			"content-type": "text/html; charset=utf-8",
			// The notice is static between deploys; cache aggressively but
			// allow revalidation so a redeploy invalidates browsers' caches
			// when content actually changes.
			"cache-control": "public, max-age=300, must-revalidate",
		},
	});
}
