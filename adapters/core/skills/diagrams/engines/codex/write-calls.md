That pruning only fires if the write lands under the right name, so write **one
`.d2` per tool call, named by absolute path**. The hook reads the paths an
`apply_patch` call adds or updates (`*** Add File:` / `*** Update File:`),
resolved against the *project* cwd (write the absolute path even though `apply_patch` prefers relative ones: a path containing `..` renders but is never adopted); every `.d2` the patch names renders. A `.d2`
written by a shell command (heredoc, `sed -i`) is not picked up — use
`apply_patch`.
