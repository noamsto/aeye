When structure is clearer seen than read, write a [D2](https://d2lang.com)
diagram as a `.d2` file with the `Write` tool. A `postToolUse` hook renders it
browser-free (`aeye render-diagram` → svg → resvg → png) into the per-pane image
manifest, and the carousel shows it like any other image — auto-opening once per
session.
