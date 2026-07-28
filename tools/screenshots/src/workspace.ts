import { cp, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

// The env block the overlay substitutes for demo/'s cloud environment. A
// local default keeps every live surface pointed at the seeded KurrentDB the
// harness (and CI) stands up. The connection string is visible in shots, so
// the default stays the canonical localhost form; GAFFER_SCREENSHOTS_DB
// exists for iterating inside a devcontainer, where the compose port binds
// on the docker host (172.17.0.1), not the container's localhost.
const connection =
  process.env.GAFFER_SCREENSHOTS_DB ?? "kurrentdb://localhost:2113?tls=false";
const localEnv = `[env.local]
connection = ${JSON.stringify(connection)}
default = true`;

/**
 * Copy demo/ into dest and rewrite its gaffer.toml so [env.local] (pointing
 * at the harness's KurrentDB) replaces the cloud environment. demo/ itself
 * stays pristine: cloud default, no database required, as its README promises.
 */
export async function overlayDemoWorkspace(
  repoRoot: string,
  dest: string,
): Promise<string> {
  await cp(join(repoRoot, "demo"), dest, {
    recursive: true,
    filter: (src) => !/\/(\.gaffer|\.env[^/]*|node_modules)$/.test(src),
  });

  const tomlPath = join(dest, "gaffer.toml");
  const toml = await readFile(tomlPath, "utf8");
  const lines = toml.split("\n");
  const start = lines.findIndex((l) => l.trim() === "[env.cloud]");
  if (start === -1)
    throw new Error("demo/gaffer.toml has no [env.cloud] block to overlay");
  let end = start + 1;
  while (end < lines.length && !lines[end]!.trimStart().startsWith("[")) end++;
  // Back off trailing blank and comment lines: a comment sitting above the
  // next block belongs to that block, not to the env being replaced.
  while (
    end > start + 1 &&
    (lines[end - 1]!.trim() === "" || lines[end - 1]!.trim().startsWith("#"))
  )
    end--;
  lines.splice(start, end - start, ...localEnv.split("\n"));
  await writeFile(tomlPath, lines.join("\n"));
  return dest;
}
