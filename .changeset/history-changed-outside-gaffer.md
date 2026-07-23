---
"@kurrent/gaffer": patch
---

`gaffer history` no longer labels every write "edited externally" on a server that doesn't return gaffer's tool metadata (such as the V2 projection engine). A content change now reads as an out-of-band edit only when a gaffer write precedes it. So a server that never round-trips metadata reads neutrally, as do edits made before gaffer took over.

The `history --json` classification changes to match. The metadata-less content-change kind is now `updated` (was `edited-externally`). The out-of-band flag is `outOfBand` (was `external`), true for any non-gaffer write once gaffer has been managing the projection.
