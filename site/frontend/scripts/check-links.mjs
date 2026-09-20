// Verify every internal link in the static export actually resolves — both the
// page and, when the link carries a #fragment, the anchor on that page.
//
// Why this exists: the docs ship in 5 locales and `rehype-slug` derives heading
// ids from the *translated* heading text. A cross-page link copied from the
// English source into ru/de/fr/es keeps the English fragment and silently points
// at an anchor that does not exist — nothing fails, the reader just lands at the
// top of the page. Only the built HTML knows the real ids, so the check runs
// against out/ after `next build`.
//
// Run after a build (`npm run check-links`); cwd is site/frontend. Node built-ins
// only — a regex scan over the export is enough and keeps the dep tree unchanged.
import { readdirSync, readFileSync, statSync, existsSync } from "node:fs";
import { join, resolve, posix } from "node:path";

const exportDir = resolve(process.cwd(), process.argv[2] ?? "out");
const basePath = (process.env.NEXT_BASE_PATH ?? "").replace(/\/$/, "");

if (!existsSync(exportDir)) {
  console.error(`[check-links] export not found: ${exportDir} — run \`npm run build\` first.`);
  process.exit(1);
}

/** Every *.html under the export, as paths relative to it (posix separators). */
function htmlPages(dir, base = "") {
  const out = [];
  for (const entry of readdirSync(dir).sort()) {
    const abs = join(dir, entry);
    const rel = base ? posix.join(base, entry) : entry;
    if (statSync(abs).isDirectory()) out.push(...htmlPages(abs, rel));
    else if (entry.endsWith(".html")) out.push(rel);
  }
  return out;
}

// <script>/<style> bodies are stripped before scanning: Next inlines its RSC
// flight payload into <script>, and a stray id/href in there is not a real
// anchor or a real link.
const INERT = /<(script|style)\b[^>]*>[\s\S]*?<\/\1\s*>/gi;
// Attribute values may be double- or single-quoted (unquoted is not supported —
// nothing in the export emits them).
const ID_ATTR = /\sid\s*=\s*(?:"([^"]*)"|'([^']*)')/gi;
const HREF_ATTR = /\shref\s*=\s*(?:"([^"]*)"|'([^']*)')/gi;
const ANCHOR_TAG = /<a\s[^>]*>/gi;
const NAME_ATTR = /\sname\s*=\s*(?:"([^"]*)"|'([^']*)')/i;

/** Minimal HTML-entity decode — enough for what a Next export puts in attributes. */
function decodeEntities(s) {
  return s
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(Number(d)))
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&amp;/g, "&");
}

/** A fragment may be percent-encoded in the href but literal UTF-8 in the id. */
function fragmentForms(s) {
  const forms = new Set([s]);
  try {
    forms.add(decodeURIComponent(s));
  } catch {
    /* malformed %-escape: compare the raw form only */
  }
  return forms;
}

const anchorCache = new Map();

/** Ids and <a name> anchors on one export page, keyed by its relative path. */
function anchorsOf(relPath) {
  let set = anchorCache.get(relPath);
  if (set) return set;
  set = new Set();
  const html = readFileSync(join(exportDir, relPath), "utf8").replace(INERT, "");
  for (const m of html.matchAll(ID_ATTR)) set.add(decodeEntities(m[1] ?? m[2]));
  for (const tag of html.matchAll(ANCHOR_TAG)) {
    const n = NAME_ATTR.exec(tag[0]);
    if (n) set.add(decodeEntities(n[1] ?? n[2]));
  }
  anchorCache.set(relPath, set);
  return set;
}

/** URL path a page is served at: out/en/docs/foo/index.html -> /en/docs/foo/ */
function urlOf(relPath) {
  return basePath + "/" + relPath.replace(/(^|\/)index\.html$/, "$1");
}

/**
 * Resolve a site-absolute URL path to a file in the export. `trailingSlash: true`
 * makes `route/index.html` the norm, but a link may be written either way (and
 * may point at a plain asset), so all three shapes are accepted.
 */
function resolveTarget(urlPath) {
  const rooted = basePath && (urlPath === basePath || urlPath.startsWith(basePath + "/"))
    ? urlPath.slice(basePath.length)
    : urlPath;
  const clean = rooted.replace(/^\/+/, "");
  if (clean === "") return "index.html";
  const candidates = clean.endsWith("/")
    ? [clean + "index.html"]
    : [clean, posix.join(clean, "index.html"), clean + ".html"];
  for (const c of candidates) {
    const abs = join(exportDir, c);
    if (existsSync(abs) && statSync(abs).isFile()) return c;
  }
  return null;
}

const pages = htmlPages(exportDir);
const broken = new Map(); // source page -> [{ href, reason }]
let linkCount = 0;

for (const page of pages) {
  const html = readFileSync(join(exportDir, page), "utf8").replace(INERT, "");
  const pageUrl = urlOf(page);
  const seen = new Set();

  for (const m of html.matchAll(HREF_ATTR)) {
    const href = decodeEntities(m[1] ?? m[2]).trim();
    // External, protocol-relative and non-navigational schemes are out of scope.
    if (href === "" || href.startsWith("//") || /^[a-z][a-z0-9+.-]*:/i.test(href)) continue;
    if (seen.has(href)) continue;
    seen.add(href);
    linkCount++;

    const hash = href.indexOf("#");
    const fragment = hash === -1 ? "" : href.slice(hash + 1);
    const pathPart = (hash === -1 ? href : href.slice(0, hash)).split("?")[0];

    // Empty path means "this page"; otherwise resolve relative hrefs against the
    // directory the page is served from.
    let target = page;
    if (pathPart !== "") {
      const abs = pathPart.startsWith("/")
        ? pathPart
        : posix.resolve(posix.dirname(pageUrl + "x"), pathPart) + (pathPart.endsWith("/") ? "/" : "");
      target = resolveTarget(abs);
      if (target === null) {
        report(page, href, `no such page in the export (looked for ${abs})`);
        continue;
      }
    }

    if (fragment === "" || fragment === "top") continue; // "#top" is always valid
    if (!target.endsWith(".html")) continue; // fragment on a non-HTML asset

    const anchors = anchorsOf(target);
    const hit = [...fragmentForms(fragment)].some((f) => anchors.has(f));
    if (!hit) report(page, href, `no id="${fragment}" on ${target}`);
  }
}

function report(page, href, reason) {
  if (!broken.has(page)) broken.set(page, []);
  broken.get(page).push({ href, reason });
}

if (broken.size === 0) {
  console.log(`[check-links] checked ${pages.length} pages, ${linkCount} links, 0 broken`);
  process.exit(0);
}

const total = [...broken.values()].reduce((n, l) => n + l.length, 0);
console.error(`[check-links] ${total} broken link(s) across ${broken.size} page(s):\n`);
for (const [page, links] of [...broken].sort()) {
  console.error(`  ${page}`);
  for (const { href, reason } of links) console.error(`    ${href}\n      ${reason}`);
  console.error("");
}
console.error(`[check-links] checked ${pages.length} pages, ${linkCount} links, ${total} broken`);
process.exit(1);
