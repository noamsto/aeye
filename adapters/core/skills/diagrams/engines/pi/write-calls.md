That pruning only fires if the write lands under the right name, so write **one
`.d2` per tool call, named by absolute path**. The hook takes the file the call
names: an explicit `file_path`, else the first `.d2` token in the command that exists on disk,
resolved against the *project* cwd. A call naming two `.d2` files renders the
first only; a path made relative by an earlier `cd` resolves to nothing and
renders nothing.
