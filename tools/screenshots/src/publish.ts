import { mkdir, readdir, rm } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);
const outDir = join(repoRoot, "tools", "screenshots", "out");
const publishDir = join(repoRoot, "docs", "public", "vscode");

// Near-lossless keeps UI text crisp; the docs pay roughly 100KB per shot
// (both theme variants load, per UI-1899), so the size matters.
const webpOptions = { quality: 95, effort: 6 } as const;

/**
 * Encode every captured PNG in out/ into docs/public/vscode/ as WebP, and
 * prune published frames whose scenario no longer exists. Kept separate
 * from capture so local iteration runs (odd connection strings, partial
 * scenario filters) never touch the committed media by accident.
 */
async function main(): Promise<void> {
  const captured = (await readdir(outDir).catch(() => [] as string[]))
    .filter((f) => f.endsWith(".png") && !f.endsWith("-FAILED.png"))
    .sort();
  if (!captured.length) {
    throw new Error(`${outDir} has no captured shots - run capture first`);
  }

  await mkdir(publishDir, { recursive: true });
  for (const png of captured) {
    const webp = png.replace(/\.png$/, ".webp");
    await sharp(join(outDir, png))
      .webp(webpOptions)
      .toFile(join(publishDir, webp));
    console.log(`published ${webp}`);
  }

  // A renamed or removed scenario must not leave its old frames on the
  // site; the published dir mirrors the captured set exactly.
  const expected = new Set(captured.map((f) => f.replace(/\.png$/, ".webp")));
  for (const existing of await readdir(publishDir)) {
    if (existing.endsWith(".webp") && !expected.has(existing)) {
      await rm(join(publishDir, existing));
      console.log(`pruned stale ${existing}`);
    }
  }
}

await main();
