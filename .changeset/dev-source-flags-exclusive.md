---
"@kurrent/gaffer": patch
---

`gaffer dev` now rejects contradictory source flags instead of silently dropping one. An offline source (`--fixture` / `--events`) can't be combined with a live target (`--env` / `--connection`). Previously `gaffer dev p --fixture happy --env cloud` ran the fixture and ignored `--env`. `--env` and `--connection` may still be combined, where `--connection` overrides `--env`.
