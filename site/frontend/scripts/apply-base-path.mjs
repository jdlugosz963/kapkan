// Next rewrites its own routes and assets for basePath, but MDX links are raw
// HTML anchors in the static export. Prefix their site-absolute docs links so
// a fork published as a GitHub Pages project works below /<repository>/ too.
import { readdirSync, readFileSync, writeFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";

const basePath = (process.env.NEXT_BASE_PATH ?? "").replace(/\/$/, "");
if (!basePath) process.exit(0);
if (!basePath.startsWith("/")) throw new Error("NEXT_BASE_PATH must start with '/'");

const out = resolve(process.cwd(), "out");
function visit(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) visit(path);
    else if (name.endsWith(".html")) {
      const source = readFileSync(path, "utf8");
      const rewritten = source.replaceAll('href="/docs', `href="${basePath}/docs`);
      if (rewritten !== source) writeFileSync(path, rewritten);
    }
  }
}
visit(out);