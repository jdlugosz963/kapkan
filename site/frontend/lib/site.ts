// Site-wide constants. Kept in one place so branding/links are easy to swap
// when the landing-page design handoff lands.
export const site = {
  name: "Kapkan",
  // No longer "RTBH mitigation": since 1.4.0 announcing a route is one of two
  // backends, the other being an in-kernel XDP drop. This string is the <title>
  // suffix for every page (app/layout.tsx), so it stays short.
  tagline: "Free, open-source DDoS detection & mitigation",
  // No `version` here on purpose: it is derived from CHANGELOG.md at build time
  // by lib/version.server.ts. This module is imported by a Client Component
  // (components/docs/DocsChrome.tsx), so it must stay free of node: imports.
  // Public GitHub repository. The Go module path is github.com/kapkan-io/kapkan
  // (see go.mod), but the repo is published under fornex/kapkan — keep this in
  // sync with the actual repository URL.
  repo: "https://github.com/jdlugosz963/kapkan",
} as const;
