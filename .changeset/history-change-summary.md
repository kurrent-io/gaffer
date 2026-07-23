---
"@kurrent/gaffer": patch
---

`gaffer history --json` now carries a `changeSummary` field on a metadata-less `updated` entry, naming what moved (e.g. `query changed`). It's the same summary the terminal history already prints, so a consumer no longer has to fetch and diff the two versions' source to describe the change.
