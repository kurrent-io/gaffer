import { mkdir, readdir, rm } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";

import { scenarios } from "./scenarios.js";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);
const outDir = join(repoRoot, "tools", "screenshots", "out");
const publishDir = join(repoRoot, "docs", "public", "vscode");

// Lossy q95 keeps UI text crisp at these sizes; the docs pay roughly 100KB
// per shot (both theme variants load), so the size matters. If text ever
// fringes, nearLossless or smartSubsample are the next knobs.
const webpOptions = { quality: 95, effort: 6 } as const;

/**
 * Encode every captured PNG in out/ into docs/public/vscode/ as WebP, and
 * prune published frames whose scenario no longer exists. Kept separate
 * from capture so local iteration runs (odd connection strings, partial
 * scenario filters) never touch the committed media by accident - and the
 * gate below makes that a guarantee, not a convention: publishing demands
 * exactly the full scenario set, freshly green.
 */
async function main(): Promise<void> {
  const listing = await readdir(outDir).catch(() => [] as string[]);
  const failed = listing.filter((f) => f.endsWith("-FAILED.png"));
  if (failed.length) {
    throw new Error(
      `refusing to publish with failure shots present: ${failed.join(", ")}`,
    );
  }
  const captured = listing.filter((f) => f.endsWith(".png")).sort();
  const expected = scenarios
    .flatMap((s) => [`vscode-${s.name}-light.png`, `vscode-${s.name}-dark.png`])
    .sort();
  if (captured.join() !== expected.join()) {
    throw new Error(
      `out/ does not match the scenario set - run a full capture first\n` +
        `  have: ${captured.join(", ") || "(nothing)"}\n` +
        `  want: ${expected.join(", ")}`,
    );
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
  // site; the published dir mirrors the scenario set exactly.
  const keep = new Set(captured.map((f) => f.replace(/\.png$/, ".webp")));
  for (const existing of await readdir(publishDir)) {
    if (existing.endsWith(".webp") && !keep.has(existing)) {
      await rm(join(publishDir, existing));
      console.log(`pruned stale ${existing}`);
    }
  }
}

await main();
