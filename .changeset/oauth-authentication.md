---
"@kurrent/gaffer": patch
---

Gaffer can now authenticate to KurrentDB with OAuth/OIDC bearer tokens. An `[env.<name>.oauth]` block configures the issuer and client ID. For interactive use, `gaffer auth --env <name>` signs in through the browser and stores a token that refreshes automatically; `gaffer auth --clear` removes stored tokens. For CI, setting `KURRENTDB_OAUTH_CLIENT_SECRET` selects the non-interactive client-credentials grant. `GAFFER_NO_OPEN` prints the sign-in URL instead of opening a browser, and `GAFFER_KEYRING_PASSWORD` supplies the keyring passphrase where there's no terminal to prompt on.
