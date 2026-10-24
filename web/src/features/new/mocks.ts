// SPDX-License-Identifier: AGPL-3.0-only

// Demo fixtures for the create flows, used only with `npm run dev:mocks`.
// The generated handlers answer with one "example" item per list; these
// overrides give /new, the template gallery and the template page enough
// data to look and behave like a real install.
import { http, HttpResponse } from "msw";
import type { GitHubRepo, GitHubStatus, ScanResult, TemplateDetail, TemplateServiceInfo, TemplateSummary } from "../../api/client";

const input = (name: string, label: string, opts: Partial<TemplateSummary["inputs"][number]> = {}) =>
  ({ name, label, required: false, secret: false, ...opts });
const pw = (name = "PASSWORD", label = "Password") => input(name, label, { secret: true, required: true, default: "k3V9q2xT7mPz4LwA8sDf", generate: "secret(20)" });

type Row = Omit<TemplateSummary, "inputs" | "docs"> & { inputs?: TemplateSummary["inputs"]; info: TemplateServiceInfo[] };

const T: Row[] = [
  { id: "postgres", name: "PostgreSQL", icon: "postgresql", category: "database", tagline: "Relational database with daily backups", description: "PostgreSQL 17 with a persistent volume, a private connection URL for your services and scheduled backups to your install's object storage.", services: ["postgres"], inputs: [pw(), input("DATABASE", "Database name", { default: "app" })], info: [{ name: "Postgres", image: "postgres:17-alpine", volume: "/var/lib/postgresql/data", domain: false }] },
  { id: "redis", name: "Redis", icon: "redis", category: "database", tagline: "In-memory key value store and cache", description: "Redis 7 with append-only persistence, a password and a private URL. Good for caches, queues and sessions.", services: ["redis"], inputs: [pw()], info: [{ name: "Redis", image: "redis:7-alpine", volume: "/data", domain: false }] },
  { id: "mongodb", name: "MongoDB", icon: "mongodb", category: "database", tagline: "Document database", description: "MongoDB 7 community server with a root user and a persistent volume.", services: ["mongodb"], inputs: [pw()], info: [{ name: "MongoDB", image: "mongo:7", volume: "/data/db", domain: false }] },
  { id: "mysql", name: "MySQL", icon: "mysql", category: "database", tagline: "The popular open-source SQL database", description: "MySQL 8.4 LTS with a root password, a default database and a persistent volume.", services: ["mysql"], inputs: [pw(), input("DATABASE", "Database name", { default: "app" })], info: [{ name: "MySQL", image: "mysql:8.4", volume: "/var/lib/mysql", domain: false }] },
  { id: "mariadb", name: "MariaDB", icon: "mariadb", category: "database", tagline: "Community fork of MySQL", description: "MariaDB 11 with a persistent volume.", services: ["mariadb"], inputs: [pw()], info: [{ name: "MariaDB", image: "mariadb:11", volume: "/var/lib/mysql", domain: false }] },
  { id: "clickhouse", name: "ClickHouse", icon: "clickhouse", category: "database", tagline: "Column store for analytics at speed", description: "ClickHouse server for fast analytical queries over large tables.", services: ["clickhouse"], inputs: [pw()], info: [{ name: "ClickHouse", image: "clickhouse/clickhouse-server:24", volume: "/var/lib/clickhouse", domain: false }] },
  { id: "minio", name: "MinIO", icon: "minio", category: "storage", tagline: "S3 compatible object storage bucket", description: "MinIO with a console, a root user and a persistent volume. Point any S3 client at its private URL.", services: ["minio"], inputs: [input("ROOT_USER", "Root user", { default: "admin", required: true }), pw("ROOT_PASSWORD", "Root password")], info: [{ name: "MinIO", image: "minio/minio:latest", volume: "/data", domain: true }] },
  { id: "meilisearch", name: "Meilisearch", icon: "meilisearch", category: "storage", tagline: "Fast, typo tolerant search engine", description: "Meilisearch with a master key and a persistent index volume.", services: ["meilisearch"], inputs: [pw("MASTER_KEY", "Master key")], info: [{ name: "Meilisearch", image: "getmeili/meilisearch:v1.11", volume: "/meili_data", domain: true }] },
  { id: "n8n", name: "n8n", icon: "n8n", category: "app", tagline: "Workflow automation for technical teams", description: "n8n with a Postgres database for workflows and credentials, a public domain and webhook URL set for you.", services: ["n8n", "postgres"], inputs: [pw("DB_PASSWORD", "Database password")], info: [{ name: "n8n", image: "n8nio/n8n:latest", domain: true, dependsOn: ["Postgres"] }, { name: "Postgres", image: "postgres:17-alpine", volume: "/var/lib/postgresql/data", domain: false }] },
  { id: "ghost", name: "Ghost", icon: "ghost", category: "app", tagline: "Publishing platform for blogs and newsletters", description: "Ghost 5 with MySQL, a public domain and persistent content storage.", services: ["ghost", "mysql"], inputs: [pw("DB_PASSWORD", "Database password")], info: [{ name: "Ghost", image: "ghost:5-alpine", volume: "/var/lib/ghost/content", domain: true, dependsOn: ["MySQL"] }, { name: "MySQL", image: "mysql:8.4", volume: "/var/lib/mysql", domain: false }] },
  { id: "wordpress", name: "WordPress", icon: "wordpress", category: "app", tagline: "Website and blog with MariaDB", description: "WordPress with MariaDB and persistent uploads.", services: ["wordpress", "mariadb"], inputs: [pw("DB_PASSWORD", "Database password")], info: [{ name: "WordPress", image: "wordpress:6-apache", volume: "/var/www/html", domain: true, dependsOn: ["MariaDB"] }, { name: "MariaDB", image: "mariadb:11", volume: "/var/lib/mysql", domain: false }] },
  { id: "umami", name: "Umami", icon: "umami", category: "app", tagline: "Privacy friendly web analytics", description: "Umami analytics with Postgres.", services: ["umami", "postgres"], inputs: [pw("APP_SECRET", "App secret")], info: [{ name: "Umami", image: "ghcr.io/umami-software/umami:postgresql-latest", domain: true, dependsOn: ["Postgres"] }, { name: "Postgres", image: "postgres:17-alpine", volume: "/var/lib/postgresql/data", domain: false }] },
  { id: "uptime-kuma", name: "Uptime Kuma", icon: "uptimekuma", category: "app", tagline: "Self-hosted uptime monitoring", description: "Uptime Kuma with a persistent data volume and a public dashboard.", services: ["uptime-kuma"], info: [{ name: "Uptime Kuma", image: "louislam/uptime-kuma:1", volume: "/app/data", domain: true }] },
  { id: "metabase", name: "Metabase", icon: "metabase", category: "app", tagline: "Dashboards and questions for your data", description: "Metabase with its own Postgres application database.", services: ["metabase", "postgres"], inputs: [pw("DB_PASSWORD", "Database password")], info: [{ name: "Metabase", image: "metabase/metabase:latest", domain: true, dependsOn: ["Postgres"] }, { name: "Postgres", image: "postgres:17-alpine", volume: "/var/lib/postgresql/data", domain: false }] },
  { id: "plausible", name: "Plausible", icon: "plausible", category: "app", tagline: "Lightweight, cookie free analytics", description: "Plausible Community Edition with Postgres and ClickHouse.", services: ["plausible", "postgres", "clickhouse"], inputs: [pw("SECRET_KEY_BASE", "Secret key base")], info: [{ name: "Plausible", image: "ghcr.io/plausible/community-edition:v2", domain: true, dependsOn: ["Postgres", "ClickHouse"] }, { name: "Postgres", image: "postgres:17-alpine", volume: "/var/lib/postgresql/data", domain: false }, { name: "ClickHouse", image: "clickhouse/clickhouse-server:24", volume: "/var/lib/clickhouse", domain: false }] },
  { id: "grafana", name: "Grafana", icon: "grafana", category: "app", tagline: "Observability dashboards", description: "Grafana OSS with a persistent volume for dashboards and plugins.", services: ["grafana"], inputs: [pw("ADMIN_PASSWORD", "Admin password")], info: [{ name: "Grafana", image: "grafana/grafana-oss:11", volume: "/var/lib/grafana", domain: true }] },
  { id: "nodejs", name: "Node.js", icon: "nodedotjs", category: "app", tagline: "A minimal Node.js web server", description: "A starter Node.js HTTP server that listens on $PORT.", services: ["node"], info: [{ name: "Node", image: "node:22-alpine", domain: true }] },
  { id: "vaultwarden", name: "Vaultwarden", icon: "vaultwarden", category: "app", tagline: "Password manager server", description: "Vaultwarden, a lightweight Bitwarden compatible server.", services: ["vaultwarden"], info: [{ name: "Vaultwarden", image: "vaultwarden/server:latest", volume: "/data", domain: true }] },
];

const summary = (r: Row): TemplateSummary => ({
  id: r.id, name: r.name, tagline: r.tagline, description: r.description, icon: r.icon, category: r.category,
  services: r.services, inputs: r.inputs || [], docs: `https://docs.example.com/templates/${r.id}`,
});

const REPOS: GitHubRepo[] = [
  "acme/web", "acme/api", "acme/worker", "acme/docs", "acme/marketing-site", "acme/payments", "acme/mobile-backend",
  "acme/infra", "acme/analytics", "acme/admin-dashboard", "ada/notes", "ada/blog", "ada/dotfiles", "ada/rust-playground",
].map((fullName, i) => ({ fullName, private: i % 3 === 0, defaultBranch: "main", updatedAt: 1767225600000 - i * 86400000 }));

const SCAN: ScanResult = {
  builder: "railpack", provider: "node", dockerfile: false, startCommand: "node server.js", suggestedPort: 3000,
  envNames: ["DATABASE_URL", "SESSION_SECRET"], configFiles: [],
} as ScanResult;

let installed: Promise<void> | undefined;

/** Registers the overrides once on the running MSW worker. */
export function install(): Promise<void> {
  return (installed ||= import("../../api/mocks/browser").then(({ worker }) => {
    worker.use(
      http.get("*/api/v1/templates", ({ request }) => {
        const q = new URL(request.url).searchParams.get("q")?.toLowerCase() || "";
        return HttpResponse.json(T.filter((t) => !q || `${t.name} ${t.tagline} ${t.id}`.toLowerCase().includes(q)).map(summary));
      }),
      http.get("*/api/v1/templates/:id", ({ params }) => {
        const r = T.find((t) => t.id === params.id);
        if (!r) return HttpResponse.json({ error: "No template with that id.", code: "not_found" }, { status: 404 });
        return HttpResponse.json({ ...summary(r), majorVersions: [], serviceInfo: r.info } satisfies TemplateDetail);
      }),
      http.get("*/api/v1/github", () => HttpResponse.json({
        appConfigured: true, appSlug: "fifty-console", configureUrl: "https://github.com/settings/installations", installUrl: "https://github.com/apps/fifty-console/installations/new", installations: ["acme", "ada"],
      } satisfies GitHubStatus)),
      http.get("*/api/v1/github/repos", async () => { await new Promise((r) => setTimeout(r, 250)); return HttpResponse.json(REPOS); }),
      http.get("*/api/v1/github/branches", () => HttpResponse.json(["main", "develop", "release"])),
      http.post("*/api/v1/github/scan", async () => { await new Promise((r) => setTimeout(r, 400)); return HttpResponse.json(SCAN); }),
    );
  }));
}
