---
"@kurrent/gaffer-runtime": minor
---

**Breaking:** `setState` and the state getters now surface runtime errors instead of swallowing them. `setState` throws when the runtime rejects the state, where a failed restore was previously silent. `getState`, `getSharedState`, and `getPartitionKey` throw on a genuine runtime error (such as a throwing `partitionBy`) rather than returning `null`. A `null` result now means only not-seen or not-applicable. The native runtime also rejects a null state passed to `setState`.
