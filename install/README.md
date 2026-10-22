# install

`install.sh` is the one-command installer (docs/SPEC.md section 4), POSIX
`sh`. `systemd/` holds the units the release tarball ships and the installer
copies to `/etc/systemd/system`.

```
curl -fsSL https://5050cloud.com/install.sh | sudo sh
wget -qO- https://5050cloud.com/install.sh | sudo sh
```

Piped it asks nothing; run from a terminal it asks for the domain, console
address and email. Every answer is also a flag and a `FIFTY_*` variable
(`--help` lists them).

Release signatures are checked with `minisign` when it is installed, else
with OpenSSL 3 (Ed25519 over the BLAKE2b-512 digest, as minisign does).

Later work (not in v0.1.0): switching the console to Caddy's internal CA when
no public certificate can be issued in 120 s (today the installer warns and
keeps retrying the public certificate), and the `--ca-pin` flow for that case.
