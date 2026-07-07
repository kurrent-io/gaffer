---
"@kurrent/gaffer": patch
---

The `[database_config]` drift check now runs on OAuth and certificate-auth environments. The node-options read authenticates exactly like the connection itself: an OAuth bearer token (client-credentials or the login stored by `gaffer auth`, never prompting), or the environment's user certificate presented in the TLS handshake, honouring the connection's `tlsCaFile` and `tlsVerifyCert` settings.
