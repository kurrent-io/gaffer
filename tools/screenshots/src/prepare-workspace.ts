import { appendFile, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { seedDeployState } from "./seed.js";
import { connection, overlayDemoWorkspace } from "./workspace.js";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);

// A stable, gitignored path: the vhs deploy tapes run from here, so the
// workflow and the tapes' regen instructions can name it without plumbing
// a temp dir through.
const dest = join(repoRoot, "tools", "screenshots", ".workspace");

// Remove a [[projection]] block by name. The tape workspace drops demo's
// deliberately-invalid projections: a whole-plan `gaffer deploy` refuses
// while any projection is invalid, and the deploy tape exists to show the
// plan -> confirm -> apply loop, not the refusal.
function stripProjection(toml: string, name: string): string {
  const lines = toml.split("\n");
  for (let i = 0; i < lines.length; i++) {
    if (lines[i]!.trim() !== "[[projection]]") continue;
    let end = i + 1;
    while (end < lines.length && !lines[end]!.trimStart().startsWith("["))
      end++;
    if (!lines.slice(i, end).some((l) => l.includes(`name = "${name}"`)))
      continue;
    while (end < lines.length && lines[end]!.trim() === "") end++;
    lines.splice(i, end - i);
    return lines.join("\n");
  }
  throw new Error(`no [[projection]] ${name} in the tape workspace toml`);
}

// Overlay demo/ and seed the partial profile: one projection left to
// create and one drifted, so a recorded `gaffer deploy` has a real plan.
// [env.staging] aliases the same server, giving the --env staging tape a
// named target and a confirm prompt without needing a second database.
async function main(): Promise<void> {
  await rm(dest, { recursive: true, force: true });
  await overlayDemoWorkspace(repoRoot, dest);

  const tomlPath = join(dest, "gaffer.toml");
  let toml = await readFile(tomlPath, "utf8");
  toml = stripProjection(toml, "broken");
  toml = stripProjection(toml, "bistate-counter");
  await writeFile(tomlPath, toml);

  await appendFile(
    tomlPath,
    `\n[env.staging]\nconnection = ${JSON.stringify(connection)}\n`,
  );
  await seedDeployState(repoRoot, dest, "partial");

  // An in-place line edit on top of the seeded drift, so the diff still
  // shows an intraline changed span, not just appended lines.
  const entry = join(dest, "projections", "order-count.js");
  const source = await readFile(entry, "utf8");
  const edited = source.replace(
    "state.totalCents += event.body.cents;",
    "state.totalCents += event.body.cents ?? 0;",
  );
  if (edited === source)
    throw new Error("order-count.js line to edit not found");
  await writeFile(entry, edited);

  console.log(dest);
}

await main();
