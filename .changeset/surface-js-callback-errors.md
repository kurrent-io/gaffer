---
"@kurrent/gaffer-runtime": minor
---

**Breaking:** a throw inside an `onEmit`, `onLog`, or `onStateChanged` callback now surfaces from the `feed`, `getResult`, or `getPartitionKey` call that triggered it, instead of being swallowed at the koffi FFI boundary where a consumer could silently lose events.
