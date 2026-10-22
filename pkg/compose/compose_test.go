// SPDX-License-Identifier: MIT

package compose

import (
	"strings"
	"testing"
)

const webPgRedis = `
name: shop
version: "3.9"
services:
  web:
    build:
      context: ./app
      dockerfile: Dockerfile.prod
      args: {A: 1}
    command: ["node", "server.js", "--port", "3000"]
    ports: ["8080:3000", "9229:9229"]
    environment:
      DATABASE_URL: postgres://postgres:pw@db:5432/shop
      LOG_LEVEL: ${LOG_LEVEL:-info}
      API_KEY: ${API_KEY}
      LITERAL: cost $$5
    env_file: [.env, missing.env]
    depends_on:
      db: {condition: service_healthy}
      cache: {condition: service_started}
    deploy:
      replicas: 2
      resources:
        limits: {cpus: "0.5", memory: 512M}
      placement: {constraints: []}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health?deep=1"]
    restart: unless-stopped
    volumes:
      - uploads:/app/uploads
      - ./local:/app/local
      - other:/app/other
    container_name: shop-web
    privileged: true
    user: "1000:1000"
    shm_size: 1gb
    stop_grace_period: 1m
    mystery: 1
  db:
    image: postgres:16-alpine
    environment:
      - POSTGRES_PASSWORD=pw
      - POSTGRES_DB=shop
    volumes: [pgdata:/var/lib/postgresql/data]
    ports: ["5432:5432"]
  cache:
    image: redis:7
    restart: "on-failure:3"
  worker:
    image: ghcr.io/acme/worker:1.2
    entrypoint: ["/bin/run"]
    command: "--queue default"
    depends_on: [cache, ghost]
    ports: [{target: 5672, published: 5672}]
    volumes:
      - type: volume
        source: jobs
        target: /jobs
    healthcheck:
      test: ["CMD-SHELL", "pgrep worker"]
networks:
  default: {}
volumes:
  uploads: {}
  pgdata: {}
`

func find(p *Project, name string) *Service {
	for i := range p.Services {
		if p.Services[i].Name == name {
			return &p.Services[i]
		}
	}
	return nil
}

func skippedFor(p *Project, prefix string) []string {
	var out []string
	for _, s := range p.Skipped {
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s.Name+": "+s.Detail)
		}
	}
	return out
}

func TestParseFixture(t *testing.T) {
	p, err := Parse([]byte(webPgRedis), Options{EnvFiles: map[string]string{".env": "# x\nexport FROM_FILE=\"yes\"\nOTHER=1\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "shop" {
		t.Fatalf("name = %q", p.Name)
	}
	var order []string
	for _, s := range p.Services {
		order = append(order, s.Name)
	}
	if strings.Join(order, ",") != "cache,db,web,worker" {
		t.Fatalf("order = %v", order)
	}
	web := find(p, "web")
	c := web.Config
	if c.Source.Type != "empty" || c.Source.RootDir != "/app" || c.Build.Builder != "dockerfile" || c.Build.DockerfilePath != "Dockerfile.prod" {
		t.Fatalf("source/build = %+v %+v", c.Source, c.Build)
	}
	if c.Deploy.StartCommand != "node server.js --port 3000" || c.Net.Port != 3000 || !web.Domain || web.TCPProxyPort != 0 {
		t.Fatalf("web = %+v domain=%v", c, web.Domain)
	}
	if c.Deploy.Replicas != 2 || c.Deploy.CPULimit != 0.5 || c.Deploy.MemoryLimitMB != 512 || c.Deploy.HealthcheckPath != "/health?deep=1" || c.Deploy.RestartPolicy != "always" {
		t.Fatalf("deploy = %+v", c.Deploy)
	}
	if c.Deploy.RunAsUser != "1000:1000" || c.Deploy.ShmSizeMB != 1024 || c.Deploy.DrainingSec != 60 {
		t.Fatalf("deploy extras = %+v", c.Deploy)
	}
	if web.Variables["LOG_LEVEL"] != "info" || web.Variables["API_KEY"] != "${API_KEY}" || web.Variables["LITERAL"] != "cost $5" || web.Variables["FROM_FILE"] != "yes" {
		t.Fatalf("variables = %v", web.Variables)
	}
	if web.Volume == nil || web.Volume.Name != "uploads" || web.Volume.MountPath != "/app/uploads" {
		t.Fatalf("volume = %+v", web.Volume)
	}
	if strings.Join(web.DependsOn, ",") != "db,cache" && strings.Join(web.DependsOn, ",") != "cache,db" {
		t.Fatalf("depends = %v", web.DependsOn)
	}
	notes := strings.Join(web.Notes, "\n")
	if !strings.Contains(notes, "API_KEY uses ${API_KEY}") || !strings.Contains(notes, "connect a repository") {
		t.Fatalf("notes = %v", web.Notes)
	}
	sk := strings.Join(skippedFor(p, "web."), "\n")
	for _, want := range []string{"web.build.args", "web.ports: Only the first port", "web.volumes: The bind mount ./local:/app/local", "web.volumes: A service has one volume", "web.container_name", "web.privileged", "web.mystery: This compose key is not supported.", "web.env_file: The file missing.env", "web.deploy.placement"} {
		if !strings.Contains(sk, want) {
			t.Errorf("skipped misses %q:\n%s", want, sk)
		}
	}
	db := find(p, "db")
	if db.Template != "postgres" || db.TemplateVersion != "16" || db.Config.Template != "postgres" || db.Domain || db.TCPProxyPort != 0 {
		t.Fatalf("db = %+v", db)
	}
	if _, ok := db.Variables["POSTGRES_PASSWORD"]; ok {
		t.Fatal("template credentials must be dropped")
	}
	if db.Variables["POSTGRES_DB"] != "shop" {
		t.Fatalf("db vars = %v", db.Variables)
	}
	if len(skippedFor(p, "db.volumes")) != 1 {
		t.Fatalf("db volume should be skipped for the template: %v", p.Skipped)
	}
	cache := find(p, "cache")
	if cache.Template != "redis" || cache.Config.Deploy.RestartPolicy != "on_failure" || cache.Config.Deploy.RestartMaxRetries != 3 {
		t.Fatalf("cache = %+v", cache)
	}
	w := find(p, "worker")
	if w.Config.Deploy.StartCommand != "/bin/run --queue default" || w.TCPProxyPort != 5672 || w.Volume == nil || w.Volume.Name != "jobs" {
		t.Fatalf("worker = %+v", w)
	}
	if len(skippedFor(p, "worker.depends_on")) != 1 || len(skippedFor(p, "worker.healthcheck")) != 1 {
		t.Fatalf("worker skipped = %v", p.Skipped)
	}
	if !strings.Contains(strings.Join(w.Notes, " "), "not declared under top-level volumes") {
		t.Fatalf("worker notes = %v", w.Notes)
	}
	if len(skippedFor(p, "networks")) != 1 {
		t.Fatalf("top level skipped = %v", p.Skipped)
	}
}

func TestErrorsAndEdges(t *testing.T) {
	for _, bad := range []string{"", "  ", "services: [", "- a\n- b\n", "version: '3'\n", "services: {}\n"} {
		if _, err := Parse([]byte(bad), Options{}); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	if _, err := Parse(make([]byte, MaxFileBytes+1), Options{}); err == nil {
		t.Fatal("oversized file accepted")
	}
	p, err := Parse([]byte(`services:
  "My App!!":
    image: nginx:1.27
    ports: ["80"]
  My-App:
    build: .
  noimage:
    environment: [FLAG]
  broken: 3
  cyc1: {image: a, depends_on: [cyc2]}
  cyc2: {image: b, depends_on: [cyc1]}
  mongo: {image: "docker.io/library/mongo:8.0.3", volumes: ["/data/db"]}
  maria: {image: "mariadb:latest@sha256:abc"}
  bad: {image: x, ports: ["nope"], volumes: [{type: tmpfs, target: /t}, {}], restart: sometimes, healthcheck: {disable: true}, expose: ["9000"]}
`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if find(p, "My App") == nil || find(p, "My-App") == nil {
		t.Fatalf("names = %+v", p.Services)
	}
	if s := find(p, "My App"); s.Config.Net.Port != 80 || s.Domain || len(s.Notes) == 0 {
		t.Fatalf("unpublished port must not suggest a domain: %+v", s)
	}
	if s := find(p, "My-App"); s.Config.Source.RootDir != "/" {
		t.Fatalf("build . = %+v", s.Config.Source)
	}
	if s := find(p, "noimage"); s.Config.Source.Type != "empty" || s.Variables["FLAG"] != "" {
		t.Fatalf("noimage = %+v", s)
	}
	if len(skippedFor(p, "broken")) != 1 {
		t.Fatal("non-mapping service must be skipped")
	}
	if find(p, "cyc1") == nil || find(p, "cyc2") == nil {
		t.Fatal("cycles keep both services")
	}
	if s := find(p, "mongo"); s.Template != "mongodb" || s.TemplateVersion != "8" {
		t.Fatalf("mongo = %+v", s)
	}
	if s := find(p, "maria"); s.Template != "mysql" || s.TemplateVersion != "" {
		t.Fatalf("maria = %+v", s)
	}
	b := find(p, "bad")
	if b.Config.Net.Port != 9000 || b.Config.Deploy.RestartPolicy != "" {
		t.Fatalf("bad = %+v", b.Config)
	}
	sk := strings.Join(skippedFor(p, "bad."), "\n")
	for _, want := range []string{"cannot be read", "bind mount", "Restart policy \"sometimes\""} {
		if !strings.Contains(sk, want) {
			t.Errorf("skipped misses %q:\n%s", want, sk)
		}
	}
	if strings.Contains(sk, "healthcheck") {
		t.Fatal("a disabled health check is not a skip")
	}
}

func TestHelpers(t *testing.T) {
	if cleanName("!!!") != "service" || len(cleanName(strings.Repeat("a", 40))) != 32 {
		t.Fatal("cleanName")
	}
	if shellQuote("it's") != `'it'\''s'` || shellQuote("") != "''" || shellQuote("plain") != "plain" {
		t.Fatal("shellQuote")
	}
	for in, want := range map[string]int{"512m": 512, "1g": 1024, "2048k": 2, "1048576": 1, "1t": 1 << 20, "x": 0, "1.5gb": 1536} {
		if got := memMB(in); got != want {
			t.Errorf("memMB(%q) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[string]int{"10s": 10, "1500ms": 1, "2m": 120, "1h": 3600, "5": 5, "bad": 0} {
		if got := durationSec(in); got != want {
			t.Errorf("durationSec(%q) = %d", in, got)
		}
	}
	for in, want := range map[string]string{"always": "always", "no": "never", "on-failure": "on_failure"} {
		if got, _ := restartPolicy(in); got != want {
			t.Errorf("restartPolicy(%q) = %q", in, got)
		}
	}
	if tp, pub, ok := parsePort("127.0.0.1:8000-8001:80-81/tcp"); !ok || tp != 80 || pub != 8000 {
		t.Fatalf("range port = %d %d %v", tp, pub, ok)
	}
	if healthPath("curl http://localhost:3000") != "/" || healthPath(3) != "" || healthPath("wget http://%zz") != "" {
		t.Fatal("healthPath")
	}
	if tpl, _ := DatabaseTemplate("nginx"); tpl != "" {
		t.Fatal("nginx is not a database")
	}
	if tpl, _ := DatabaseTemplate("pgvector/pgvector:pg17"); tpl != "pgvector" {
		t.Fatal("pgvector")
	}
	if toList(map[string]any{}) != nil || toList([]any{map[string]any{"path": "a.env"}})[0] != "a.env" {
		t.Fatal("toList")
	}
	if dependsOn("x") != nil || shellJoin(3) != "" {
		t.Fatal("dependsOn/shellJoin of unexpected types")
	}
	e := &Error{"x"}
	if e.Error() != "x" {
		t.Fatal("Error")
	}
	if m := envMap(map[string]any{"N": nil, "A": 1}); m["N"] != "" || m["A"] != "1" {
		t.Fatal("envMap")
	}
	if pe := parseEnvFile("A='q'\nnoeq\n"); pe["A"] != "q" || len(pe) != 1 {
		t.Fatal("parseEnvFile")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(webPgRedis)
	f.Fuzz(func(t *testing.T, s string) { _, _ = Parse([]byte(s), Options{}) })
}
