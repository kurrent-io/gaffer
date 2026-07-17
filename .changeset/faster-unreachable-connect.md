---
"@kurrent/gaffer": patch
---

Commands that connect to KurrentDB now give up faster when an environment is unreachable: 2 node-discovery attempts instead of the client library's 10, which cuts a failed connect from ~7s to ~1s. A reachable endpoint connects on the first attempt, so this only shortens the unreachable case. Set `maxDiscoverAttempts` in the connection string to override.
