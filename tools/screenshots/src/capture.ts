import { mkdir, mkdtemp, readdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { launchVSCode, type Theme } from "./harness.js";
import { scenarios } from "./scenarios.js";
import { ensureVSCode, installVsix } from "./vscode.js";
import { overlayDemoWorkspace } from "./workspace.js";

const repoRoot = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
);
const packageRoot = join(repoRoot, "tools", "screenshots");

// Resolve the .vsix the way release-publish.yml does: from the manifest's
// name and version, never by globbing.
async function vsixPath(): Promise<string> {
  const manifest = JSON.parse(
    await readFile(join(repoRoot, "editors", "vscode", "package.json"), "utf8"),
  ) as {
    name: string;
    version: string;
  };
  const path = join(
    repoRoot,
    "editors",
    "vscode",
    `${manifest.name}-${manifest.version}.vsix`,
  );
  await readFile(path).catch(() => {
    throw new Error(`${path} not found - run \`just editors package\` first`);
  });
  return path;
}

async function main(): Promise<void> {
  const only = process.argv.slice(2);
  const unknown = only.filter(
    (name) => !scenarios.some((s) => s.name === name),
  );
  if (unknown.length) {
    throw new Error(
      `unknown scenarios: ${unknown.join(", ")} (have: ${scenarios.map((s) => s.name).join(", ")})`,
    );
  }
  const selected = only.length
    ? scenarios.filter((s) => only.includes(s.name))
    : scenarios;

  const vscode = await ensureVSCode(join(packageRoot, ".cache"));
  const outDir = join(packageRoot, "out");
  await mkdir(outDir, { recursive: true });
  // Stale failure shots from a previous run must not sit next to this
  // run's green output.
  for (const stale of await readdir(outDir)) {
    if (stale.endsWith("-FAILED.png"))
      await rm(join(outDir, stale), { force: true });
  }

  const staging = await mkdtemp(join(tmpdir(), "gaffer-screenshots-"));
  try {
    const workspace = await overlayDemoWorkspace(
      repoRoot,
      join(staging, "workspace"),
    );
    const extensionsDir = join(staging, "extensions");
    await mkdir(extensionsDir, { recursive: true });
    installVsix(
      vscode,
      await vsixPath(),
      extensionsDir,
      join(staging, "install-udd"),
    );

    for (const theme of ["light", "dark"] as Theme[]) {
      // A fresh user-data-dir per theme: the theme is baked into the
      // settings before launch, and no state leaks between runs.
      const userDataDir = join(staging, `udd-${theme}`);
      await mkdir(userDataDir, { recursive: true });
      const session = await launchVSCode({
        vscode,
        userDataDir,
        extensionsDir,
        workspace,
        theme,
        // The dev cli/gaffer binary's RUNPATH points at the in-tree
        // runtime publish dir, so this only resolves in the
        // devcontainer or CI checkout it was built in.
        pathPrepend: [join(repoRoot, "cli")],
      });
      try {
        for (const scenario of selected) {
          console.log(`capturing ${scenario.name} (${theme})`);
          try {
            await scenario.run(session.page);
          } catch (err) {
            // A shot of where the window actually is beats a bare
            // timeout - but if the window is gone the screenshot
            // fails too, and the scenario error must still win.
            await session.page
              .screenshot({
                path: join(
                  outDir,
                  `vscode-${scenario.name}-${theme}-FAILED.png`,
                ),
              })
              .catch(() => {});
            throw err;
          }
          await session.page.screenshot({
            path: join(outDir, `vscode-${scenario.name}-${theme}.png`),
          });
        }
      } finally {
        await session.app.close();
      }
    }
  } finally {
    // GAFFER_SCREENSHOTS_KEEP leaves the staging dir (workspace overlay,
    // user-data-dirs, logs) behind for debugging a failed run.
    if (process.env.GAFFER_SCREENSHOTS_KEEP) {
      console.log(`keeping staging dir: ${staging}`);
    } else {
      await rm(staging, { recursive: true, force: true });
    }
  }
  console.log(`captured ${selected.length * 2} shots into ${outDir}`);
}

await main();
