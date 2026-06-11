---
"@kurrent/gaffer-runtime": patch
---

The state serializer now raises a clear error for state it can't serialize. Deeply nested state, a circular reference, and an array with more than ~2 billion elements each surface as a state-serialization error. Previously these produced a misleading "Index was outside the bounds of the array" failure, or silently serialized an oversized array as `[]`.
