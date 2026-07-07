---
"@kurrent/gaffer": patch
---

`gaffer recreate` (and the `deploy_recreate` MCP tool) no longer strands a projection when the server is slow to settle the delete. KurrentDB deletes projections asynchronously, so the rebuild's create could bounce off the still-registered name with a Conflict and leave the projection deleted but not recreated. The create now retries over a ten-second settle window before giving up with the recovery instructions.
