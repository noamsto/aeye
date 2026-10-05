That pruning only fires if the write lands under the right name, so write **one
`.d2` per tool call, named by absolute path**. The hook reads the `file_path` of
`Write` and `Read` calls, resolved against the *project* cwd; a `.d2` created by
a `Shell` command (heredoc, `sed -i`) is not picked up — use `Write`.
