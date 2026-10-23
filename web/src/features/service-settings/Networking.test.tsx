// SPDX-License-Identifier: AGPL-3.0-only

import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { Domain } from "../../api/types.gen";
import { DomainRow } from "./Networking";

const base: Domain = { id: "dom_1", serviceId: "svc_1", domain: "shop.example.com", kind: "custom", port: 8080, dnsOk: false, certOk: false, status: "pending_dns", message: "Waiting for DNS: the name does not resolve yet" };
const records = [
  { type: "A", name: "shop.example.com", host: "shop", value: "203.0.113.7" },
  { type: "A", name: "shop.example.com", host: "shop", value: "203.0.113.8" },
  { type: "CNAME", name: "shop.example.com", host: "shop", value: "web-ab12.203-0-113-7.sslip.io" },
];

const html = (d: Domain) => renderToStaticMarkup(createElement(DomainRow, { d, onChange: () => undefined, onDelete: () => undefined }));
const copies = (h: string) => (h.match(/aria-label="Copy"/g) || []).length;

describe("DomainRow", () => {
  it("shows every DNS record with the host label and a copy button for each value", () => {
    const h = html({ ...base, dns: records });
    expect(h).toContain("Waiting for DNS");
    expect(h).toContain("203.0.113.7");
    expect(h).toContain("203.0.113.8");
    expect(h).toContain("title=\"shop.example.com\">shop ");
    // The URL, then host and value for each of the three records.
    expect(copies(h)).toBe(1 + 3 * 2);
    expect(h).not.toContain("Retry certificate");
  });

  it("shows the apex note beside the CNAME", () => {
    const h = html({ ...base, domain: "example.org", dns: [{ type: "CNAME", name: "example.org", host: "@", value: "web.sslip.io", note: "Only where your DNS provider supports CNAME flattening or an ALIAS record at the apex." }] });
    expect(h).toContain("CNAME flattening");
    expect(h).toContain("@");
  });

  it("offers a retry while the certificate is issuing, with the label for each state", () => {
    const issuing = html({ ...base, dnsOk: true, status: "issuing", message: "" });
    expect(issuing).toContain("Issuing certificate");
    expect(issuing).toContain("Retry certificate");
    expect(issuing).not.toContain("<table");
  });

  it("shows a failed certificate in red with Caddy's sentence and a retry", () => {
    const h = html({ ...base, dnsOk: true, status: "error", message: "The certificate for shop.example.com could not be issued yet: boom" });
    expect(h).toContain(">Error<");
    expect(h).toContain("text-red-600");
    expect(h).toContain("boom");
    expect(h).toContain("Retry certificate");
  });

  it("shows an active domain with its expiry and no retry", () => {
    const h = html({ ...base, dnsOk: true, certOk: true, status: "active", message: "", certExpiresAt: 4102444800000 });
    expect(h).toContain("Active");
    expect(h).toContain("Certificate valid until");
    expect(h).not.toContain("Retry certificate");
  });

  it("says once that the record points at Cloudflare", () => {
    const h = html({ ...base, dnsOk: true, cloudflare: true, status: "issuing", message: "Cloudflare proxy detected: set SSL/TLS mode to Full (strict), and turn off Always Use HTTPS until this domain is Active." });
    expect(h.match(/Cloudflare proxy detected/g)?.length).toBe(1);
    expect(h).not.toContain("points at Cloudflare's proxy");
  });
});
