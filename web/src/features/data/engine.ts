// SPDX-License-Identifier: AGPL-3.0-only

// Per-engine descriptors for the database views: label, brand icon key, the
// variables that hold the connection URLs, the default port and the client.

import type { Service } from "../../api/types.gen";

export interface Engine {
  key: "postgres" | "redis" | "mysql" | "mongodb";
  label: string;
  brand: string;
  privateVar: string;
  publicVar: string;
  port: number;
  client: string;
  /** Word for the objects shown in the Data grid. */
  noun: string;
  nouns: string;
}

export const ENGINES: Record<Engine["key"], Engine> = {
  postgres: { key: "postgres", label: "Postgres", brand: "postgresql", privateVar: "DATABASE_PRIVATE_URL", publicVar: "DATABASE_PUBLIC_URL", port: 5432, client: "psql", noun: "table", nouns: "tables" },
  redis: { key: "redis", label: "Redis", brand: "redis", privateVar: "REDIS_PRIVATE_URL", publicVar: "REDIS_PUBLIC_URL", port: 6379, client: "redis-cli", noun: "key", nouns: "keys" },
  mysql: { key: "mysql", label: "MySQL", brand: "mysql", privateVar: "MYSQL_PRIVATE_URL", publicVar: "MYSQL_PUBLIC_URL", port: 3306, client: "mysql", noun: "table", nouns: "tables" },
  mongodb: { key: "mongodb", label: "MongoDB", brand: "mongodb", privateVar: "MONGO_PRIVATE_URL", publicVar: "MONGO_PUBLIC_URL", port: 27017, client: "mongosh", noun: "collection", nouns: "collections" },
};

export function engineOf(s: Pick<Service, "config">): Engine | null {
  const t = (s.config.template || "").toLowerCase();
  if (t in ENGINES) return ENGINES[t as Engine["key"]];
  if (t === "mongo") return ENGINES.mongodb;
  return null;
}
