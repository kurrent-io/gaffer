---
"@kurrent/gaffer": patch
---

OAuth tokens are now bound to the host the environment's connection names, and gaffer only ever sends a token to the host it was obtained for. Previously a stored token was shared across every environment declaring the same issuer and client ID, so a `gaffer.toml` reusing an org's issuer/clientID but pointing its connection elsewhere would receive the user's real bearer token on any connect. An environment naming a different host now finds no token and asks for a fresh `gaffer auth` against that host instead.

Environments pointing at the same host still share one sign-in, including across projects. `gaffer auth` now resolves the environment's connection before the browser flow - a connection string that can't be expanded or parsed fails there, since there is no host to bind the token to - and its success message names the bound host. Existing stored tokens are keyed the old way and won't be found; sign in once per host (or `gaffer auth --clear` first to tidy the keyring).
