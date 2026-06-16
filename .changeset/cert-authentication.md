---
"@kurrent/gaffer": patch
---

Gaffer can now authenticate to KurrentDB with an X.509 user certificate. Set `user_cert_file` and `user_key_file` on an `[env.<name>]` block; the paths expand `${VAR}` references and resolve relative to the project root, so a relative path works from any directory. The certificate requires a TLS connection and can be combined with OAuth.
