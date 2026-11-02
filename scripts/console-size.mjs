// Run after make build-console. Reports each route's complete import closure,
// including shared runtime, using the exact precompressed production assets.
import { readFileSync } from "node:fs";
const root = new URL("../web/core-console/dist/", import.meta.url);
const manifest = JSON.parse(readFileSync(new URL(".vite/manifest.json", root), "utf8"));
function closure(key, seen = new Set()) {
  if (seen.has(key)) return [];
  seen.add(key);
  const entry = manifest[key];
  return [entry.file, ...(entry.css ?? []), ...(entry.imports ?? []).flatMap(name => closure(name, seen))];
}
for (const [label, key] of [["entry", "index.html"], ["box", "src/pages/Box.tsx"], ["overview", "src/pages/Overview.tsx"]]) {
  const files = [...new Set(closure(key))];
  const bytes = files.reduce((total, name) => total + readFileSync(new URL(name, root)).length, 0);
  const gzip = files.reduce((total, name) => total + readFileSync(new URL(name + ".gz", root)).length, 0);
  const javascript = files.filter(name => name.endsWith(".js"));
  const jsBytes = javascript.reduce((total, name) => total + readFileSync(new URL(name, root)).length, 0);
  const jsGzip = javascript.reduce((total, name) => total + readFileSync(new URL(name + ".gz", root)).length, 0);
  console.log(JSON.stringify({ label, bytes, gzip, jsBytes, jsGzip, files }));
}
