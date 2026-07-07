---
"@kurrent/gaffer": patch
---

Connection failures now name the resolution stage that failed (reading the env overlay, expanding the connection string). A certificate environment with multiple problems reports them in resolution order, so a broken `${VAR}` in a cert path surfaces before the TLS check that would follow it.
