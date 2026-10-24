# install

`install.sh` is the one-command installer (docs/SPEC.md section 4), POSIX
`sh`. `systemd/` holds the units the release tarball ships and the installer
copies to `/etc/systemd/system`.

```
curl -fsSL https://5050cloud.com/install.sh | sudo sh
wget -qO- https://5050cloud.com/install.sh | sudo sh
```

What you see: a small banner, numbered steps (`[3/8]`) with a spinner, a
progress bar and the elapsed time while each one runs, a green check or red
cross after it, and a closing box with the console address, login and next
commands. Failures name the problem and the fix; every step is safe to re-run.

Run from a terminal (`sudo sh install.sh`) it opens a short wizard: pick
install, join, CLI only or uninstall with the arrow keys (or digits) and Enter,
then answer the domain, console address, email and firewall questions (Enter
keeps the default) and confirm. Piped (`curl | sudo sh`), in CI or with
`--yes` it asks nothing and uses the flags and defaults. Colour and animation
only appear on a terminal; `NO_COLOR=1`, `--no-color`, `TERM=dumb`, a pipe or a
log file give plain text, and `CI` keeps the colour but drops the animation.

Every answer is also a flag and a `FIFTY_*` variable (`--help` lists them).
`--dry-run` walks through the screens with made up values and changes nothing,
which is handy to preview the installer. With `--domain` it checks that the
console name and the wildcard record point at the server first;
`--skip-dns-check` turns that off.

Release signatures are checked with `minisign` when it is installed, else
with OpenSSL 3 (Ed25519 over the BLAKE2b-512 digest, as minisign does).

Later work (not in v0.1.0): switching the console to Caddy's internal CA when
no public certificate can be issued in 120 s (today the installer warns and
keeps retrying the public certificate), and the `--ca-pin` flow for that case.
