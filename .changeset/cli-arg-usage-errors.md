---
"@kurrent/gaffer": patch
---

`gaffer scaffold`, `dev`, and `info` now report a missing or extra positional argument by naming the argument and showing a runnable example, instead of cobra's generic `Accepts 1 arg(s), received 0.`:

```
missing required argument <path>
example: gaffer scaffold ./projections/order.js
```

Their `--help` gains an example too, and `dev`/`info` now show the required argument as `<projection>` rather than `[projection]`.
