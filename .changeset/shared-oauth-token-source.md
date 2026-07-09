---
"@kurrent/gaffer": patch
---

OAuth environments no longer force a spurious re-sign-in when a command's connection and its config-drift check refresh the stored token at the same time. The two now share one refreshing token source per identity. A rotating identity provider (Auth0's reuse detection is the common case) can no longer reject one refresher's token as reused and discard a credential the other just rotated in.

As a side effect, the config-drift check now shares the connection's unlocked credentials on a file-keyring host, where it previously skipped when it couldn't unlock the keyring on its own.
