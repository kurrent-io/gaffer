---
"@kurrent/gaffer": patch
---

The language server emits a **Deploy** CodeLens leading each reachable `[env.X]` block in `gaffer.toml`, ahead of the deploy-status roll-up. It carries the env and the declaring `gaffer.toml` so an editor can open the deploy plan for the whole project against that env. Offered only when the env's status resolved and is authenticated (not while a fetch is in flight, on a fetch error, or when sign-in is needed).
