// SPDX-License-Identifier: AGPL-3.0-only

// Official technology logos (simple-icons, CC0) in their brand colours, for service nodes,
// pickers, templates and dashboard tiles. Unknown names fall back to a neutral box glyph.
// Near-black marks (GitHub, Next.js, Bun, Rust...) render in the text colour so they stay
// visible on dark surfaces.
import { Box, Clock, Globe, HardDrive, SquareTerminal } from "lucide-react";
import type { SimpleIcon } from "simple-icons";
import {
  siAlpinelinux, siAngular, siApachecassandra, siApachekafka, siAppsmith, siAppwrite, siAstro, siBaserow, siBitbucket,
  siBitwarden, siBudibase, siBun, siCaddy, siChatwoot, siClickhouse, siCockroachlabs, siDebian, siDeno, siDirectus,
  siDjango, siDocker, siDotnet, siElasticsearch, siElixir, siExpress, siFastapi, siFastify, siFlask, siFormbricks,
  siGatsby, siGhost, siGit, siGitea, siGithub, siGitlab, siGo, siGrafana, siHasura, siHono, siHuggingface,
  siInfluxdb, siJupyter, siKeycloak, siKubernetes, siLangchain, siLaravel, siLinux, siListmonk, siMariadb, siMattermost,
  siMeilisearch, siMetabase, siMinio, siMongodb, siMysql, siN8n, siNestjs, siNextcloud, siNextdotjs, siNginx,
  siNodedotjs, siNuxt, siOllama, siOpenjdk, siOutline, siPhoenixframework, siPhp, siPlausibleanalytics, siPocketbase,
  siPostgresql, siPosthog, siPrometheus, siPython, siQdrant, siRabbitmq, siReact, siRedis, siRemix, siRocketdotchat,
  siRuby, siRubyonrails, siRust, siSentry, siSolid, siSpring, siSqlite, siStrapi, siStreamlit, siSupabase, siSvelte,
  siTemporal, siTimescale, siTraefikproxy, siUbuntu, siUmami, siUptimekuma, siVaultwarden, siVite, siVuedotjs,
  siWordpress, siNeo4j, siMedusa, siShopify,
} from "simple-icons";

const ICONS: Record<string, SimpleIcon> = {
  alpinelinux: siAlpinelinux, angular: siAngular, cassandra: siApachecassandra, kafka: siApachekafka, appsmith: siAppsmith,
  appwrite: siAppwrite, astro: siAstro, baserow: siBaserow, bitbucket: siBitbucket, bitwarden: siBitwarden, budibase: siBudibase,
  bun: siBun, caddy: siCaddy, chatwoot: siChatwoot, clickhouse: siClickhouse, cockroachdb: siCockroachlabs, debian: siDebian,
  deno: siDeno, directus: siDirectus, django: siDjango, docker: siDocker, dotnet: siDotnet, elasticsearch: siElasticsearch,
  elixir: siElixir, express: siExpress, fastapi: siFastapi, fastify: siFastify, flask: siFlask, formbricks: siFormbricks,
  gatsby: siGatsby, ghost: siGhost, git: siGit, gitea: siGitea, github: siGithub, gitlab: siGitlab, go: siGo, grafana: siGrafana,
  hasura: siHasura, hono: siHono, huggingface: siHuggingface, influxdb: siInfluxdb, jupyter: siJupyter, keycloak: siKeycloak,
  kubernetes: siKubernetes, langchain: siLangchain, laravel: siLaravel, linux: siLinux, listmonk: siListmonk, mariadb: siMariadb,
  mattermost: siMattermost, meilisearch: siMeilisearch, metabase: siMetabase, minio: siMinio, mongodb: siMongodb, mysql: siMysql,
  n8n: siN8n, nestjs: siNestjs, nextcloud: siNextcloud, nextdotjs: siNextdotjs, nginx: siNginx, nodedotjs: siNodedotjs,
  nuxt: siNuxt, ollama: siOllama, java: siOpenjdk, outline: siOutline, phoenix: siPhoenixframework, php: siPhp,
  plausible: siPlausibleanalytics, pocketbase: siPocketbase, postgresql: siPostgresql, posthog: siPosthog,
  prometheus: siPrometheus, python: siPython, qdrant: siQdrant, rabbitmq: siRabbitmq, react: siReact, redis: siRedis,
  remix: siRemix, rocketchat: siRocketdotchat, ruby: siRuby, rails: siRubyonrails, rust: siRust, sentry: siSentry,
  solid: siSolid, spring: siSpring, sqlite: siSqlite, strapi: siStrapi, streamlit: siStreamlit, supabase: siSupabase,
  svelte: siSvelte, temporal: siTemporal, timescaledb: siTimescale, traefik: siTraefikproxy, ubuntu: siUbuntu, umami: siUmami,
  uptimekuma: siUptimekuma, vaultwarden: siVaultwarden, vite: siVite, vue: siVuedotjs, wordpress: siWordpress, neo4j: siNeo4j,
  medusa: siMedusa, shopify: siShopify,
};

/** Other spellings (image names, template ids, file names) mapped to an ICONS key. */
const ALIASES: Record<string, string> = {
  postgres: "postgresql", pg: "postgresql", psql: "postgresql", pgvector: "postgresql", postgis: "postgresql",
  pgbouncer: "postgresql", timescale: "timescaledb", mongo: "mongodb", mysql8: "mysql", maria: "mariadb",
  valkey: "redis", keydb: "redis", dragonfly: "redis", node: "nodedotjs", nodejs: "nodedotjs", "node.js": "nodedotjs",
  next: "nextdotjs", nextjs: "nextdotjs", "next.js": "nextdotjs", nuxtjs: "nuxt", vuejs: "vue", golang: "go",
  openjdk: "java", jdk: "java", "eclipse-temurin": "java", temurin: "java", rubyonrails: "rails", apachekafka: "kafka",
  apachecassandra: "cassandra", cockroach: "cockroachdb", cockroachlabs: "cockroachdb", traefikproxy: "traefik",
  "uptime-kuma": "uptimekuma", "rocket.chat": "rocketchat", rocketdotchat: "rocketchat", plausibleanalytics: "plausible",
  phoenixframework: "phoenix", vuedotjs: "vue", nodedotjs: "nodedotjs", nextdotjs: "nextdotjs", elastic: "elasticsearch",
  opensearch: "elasticsearch", meili: "meilisearch", httpd: "nginx", alpine: "alpinelinux", "minio-s3": "minio", s3: "minio",
  dotnetcore: "dotnet", aspnet: "dotnet", "aspnet-core": "dotnet", wp: "wordpress", gh: "github", repo: "github",
};

/** The ICONS key for a free-form name: "postgres", "/api/v1/icons/postgres.svg", "bitnami/redis:7", "Node.js". */
export function brandKey(name?: string | null): string | undefined {
  if (!name) return undefined;
  let n = name.trim().toLowerCase();
  if (!n) return undefined;
  n = n.split("?")[0];
  n = n.replace(/^.*\//, "");          // last path segment (image repo or icon path)
  n = n.replace(/:.*$/, "");           // image tag
  n = n.replace(/\.(svg|png|webp|jpg|jpeg)$/, "");
  if (ICONS[n]) return n;
  if (ALIASES[n]) return ALIASES[n];
  const bare = n.replace(/[^a-z0-9.]/g, "");
  if (ICONS[bare]) return bare;
  if (ALIASES[bare]) return ALIASES[bare];
  return undefined;
}

export const hasBrandIcon = (name?: string | null) => brandKey(name) !== undefined;

/** Every supported key, for pickers and tests. */
export const BRAND_KEYS = Object.keys(ICONS).sort();

/** Relative luminance of a hex colour, 0 (black) to 1 (white). */
function luminance(hex: string) {
  const c = [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}

/** The brand colour, or the text colour for near-black marks. */
export function brandColor(name?: string | null): string | undefined {
  const k = brandKey(name);
  if (!k) return undefined;
  const hex = ICONS[k].hex;
  return luminance(hex) < 0.04 ? "currentColor" : `#${hex}`;
}

export interface BrandIconProps {
  /** A brand key or alias ("postgres", "nodejs"), an icon path or an image name. */
  name?: string | null;
  /** Pixel size (square). Default 20. */
  size?: number;
  /** Render in the current text colour instead of the brand colour. */
  mono?: boolean;
  /** Fallback glyph when the name is unknown: box (default), terminal, clock, globe, volume or none. */
  fallback?: "box" | "terminal" | "clock" | "globe" | "volume" | "none";
  className?: string;
  title?: string;
}

/** An official technology logo. Decorative unless `title` is set. */
export function BrandIcon({ name, size = 20, mono, fallback = "box", className, title }: BrandIconProps) {
  const k = brandKey(name);
  if (!k) {
    if (fallback === "none") return null;
    const F = { box: Box, terminal: SquareTerminal, clock: Clock, globe: Globe, volume: HardDrive }[fallback];
    return <F size={size} strokeWidth={1.75} className={["shrink-0 text-fg-2", className].filter(Boolean).join(" ")} aria-hidden={title ? undefined : true} aria-label={title} />;
  }
  const icon = ICONS[k];
  const fill = mono ? "currentColor" : brandColor(k);
  // dim brand colours get a luminance floor in the dark theme (--brand-lift is 100% in light)
  const lift = fill && fill !== "currentColor" && luminance(fill.slice(1)) < 0.18;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill={fill} style={lift ? { fill: `color-mix(in srgb, ${fill} var(--brand-lift, 100%), white)` } : undefined} role={title ? "img" : undefined} aria-hidden={title ? undefined : true}
      aria-label={title} className={["shrink-0", fill === "currentColor" && !mono ? "text-foreground" : "", className].filter(Boolean).join(" ")}>
      {title && <title>{title}</title>}
      <path d={icon.path} />
    </svg>
  );
}

/** A 40x40 (or `size`) rounded tile holding a logo: dashboard cards, pickers, template cards. */
export function BrandTile({ name, size = 40, iconSize, fallback, className, title }: BrandIconProps & { iconSize?: number }) {
  return (
    <span className={["inline-flex shrink-0 items-center justify-center rounded-lg border border-line-popover bg-surface-hover transition-colors hover:border-line-panel", className].filter(Boolean).join(" ")}
      style={{ width: size, height: size }} title={title}>
      <BrandIcon name={name} size={iconSize ?? Math.round(size / 2)} fallback={fallback} />
    </span>
  );
}
