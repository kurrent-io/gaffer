---
"gaffer-vscode": patch
---

The extension's webviews (status, deploy-plan, history) now report client-side render failures to telemetry. A webview has no network egress of its own, so uncaught errors, unhandled rejections, and render errors caught by its error boundary are forwarded to the extension host. The host emits them as `exception` events under a new `webview` phase. Messages and stack frames are scrubbed the same as host-side exceptions; nothing from your projection code is reported.
