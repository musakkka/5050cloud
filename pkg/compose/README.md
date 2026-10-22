# pkg/compose (MIT)

Maps a `docker-compose.yml` to 5050cloud services (docs/SPEC.md 9.22): images
or builds, commands, environment and env files, ports (first one is the target
port; published ports suggest a domain or a TCP proxy), named volumes,
`depends_on` order, `deploy.replicas`, health checks and restart policies.
Well-known database images (Postgres, MySQL, Redis, MongoDB) become templates.
Everything it cannot carry over is listed with a reason, so the import report
says what was skipped and why. The control plane stages the result
(`core.ImportCompose`); nothing deploys until the user presses Deploy.
