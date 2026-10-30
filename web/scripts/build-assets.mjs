import { readFileSync, writeFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { gzipSync } from "node:zlib";

const directory = path.resolve(import.meta.dirname, "../dist");
const compressed = new Map();
for (const entry of readdirSync(path.join(directory, "assets"))) {
  if (!/\.(js|css)$/.test(entry)) continue;
  const file = `assets/${entry}`;
  const data = readFileSync(path.join(directory, file));
  const gzip = gzipSync(data, { level: 9 });
  compressed.set(file, gzip.length);
  if (data.length >= 1024 && gzip.length < data.length)
    writeFileSync(path.join(directory, `${file}.gz`), gzip);
}

// Check the entry's complete static import graph, not just one output chunk.
// Browser tests separately include the visible desktop demo's dynamic imports.
const manifest = JSON.parse(
  readFileSync(path.join(directory, ".vite/manifest.json"), "utf8"),
);
const files = new Set();
const visited = new Set();
function visit(key) {
  if (visited.has(key)) return;
  visited.add(key);
  const chunk = manifest[key];
  if (!chunk) throw new Error(`Missing manifest import: ${key}`);
  files.add(chunk.file);
  for (const css of chunk.css || []) files.add(css);
  for (const dependency of chunk.imports || []) visit(dependency);
}
const entries = Object.entries(manifest).filter(([, chunk]) => chunk.isEntry);
if (entries.length !== 1)
  throw new Error("Expected exactly one frontend entry");
visit(entries[0][0]);
for (const [extension, limit] of [
  ["js", 250 * 1024],
  ["css", 40 * 1024],
]) {
  const bytes = [...files]
    .filter((file) => file.endsWith(`.${extension}`))
    .reduce((total, file) => total + compressed.get(file), 0);
  if (!Number.isFinite(bytes) || bytes > limit)
    throw new Error(
      `Initial ${extension} gzip budget exceeded: ${bytes} > ${limit}`,
    );
  console.log(`Initial ${extension}: ${bytes} gzip bytes (limit ${limit})`);
}
