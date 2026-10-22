# node-hello

A tiny Node HTTP app. It answers JSON on `/` and `ok` on `/health`, listens on
`PORT` (default 3000) and stops cleanly on SIGTERM.

Deploy it with the CLI from this directory:

```
5050 init -n hello
5050 up
```

The end-to-end suite deploys it from a public test repository pinned to a fixed
commit, so test runs never depend on a third-party repository at HEAD.
