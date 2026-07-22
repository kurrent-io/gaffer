---
"gaffer-vscode": patch
---

The history viewer's rollback is more reliable:

- It no longer times out against a slow or remote cluster. History and rollback connect, resolve a version, and read/write, which the 10s default spawn timeout was too short for; they now get a longer one.
- The timeline refreshes as soon as a rollback lands. The refresh was previously sequenced after the success notification, which only resolves when the toast is dismissed, so the timeline appeared stale until then.
