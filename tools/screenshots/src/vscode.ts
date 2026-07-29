import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { execFileSync } from "node:child_process";

import pin from "./vscode-pin.json" with { type: "json" };

export interface VSCodePaths {
  /** The Electron binary playwright launches. */
  electron: string;
  /** The `code` CLI wrapper, for --install-extension. */
  cli: string;
  version: string;
}

// Only the platform CI and the devcontainer run on. Extend the pin file's
// sha256 map alongside this when another platform is needed.
const platform = "linux-x64";

async function exists(path: string): Promise<boolean> {
  return stat(path).then(
    () => true,
    () => false,
  );
}

async function sha256Of(path: string): Promise<string> {
  const hash = createHash("sha256");
  await pipeline(createReadStream(path), hash);
  return hash.digest("hex");
}

// Download url to dest and verify it against the pinned sha256, removing the
// file on mismatch. Everything the harness fetches goes through this gate.
async function downloadVerified(
  url: string,
  sha256: string,
  dest: string,
): Promise<void> {
  const res = await fetch(url, {
    redirect: "follow",
    signal: AbortSignal.timeout(600_000),
  });
  if (!res.ok || !res.body)
    throw new Error(`download failed: ${res.status} ${url}`);
  await pipeline(Readable.fromWeb(res.body), createWriteStream(dest));
  const got = await sha256Of(dest);
  if (got !== sha256) {
    await rm(dest, { force: true });
    throw new Error(`${url} sha256 mismatch: expected ${sha256}, got ${got}`);
  }
}

/**
 * Ensure the pinned companion extensions (theme, TOML grammar) are
 * downloaded and verified, returning their .vsix paths for install.
 */
export async function ensurePinnedExtensions(
  cacheRoot: string,
): Promise<string[]> {
  await mkdir(cacheRoot, { recursive: true });
  const paths: string[] = [];
  for (const ext of pin.extensions) {
    const dest = join(cacheRoot, `${ext.id}-${ext.version}.vsix`);
    if (!(await exists(dest))) {
      console.log(`downloading extension ${ext.id} ${ext.version}`);
      await downloadVerified(ext.url, ext.sha256, dest);
    }
    paths.push(dest);
  }
  return paths;
}

/**
 * Ensure the pinned VS Code build is downloaded, checksum-verified, and
 * extracted under cacheRoot. Idempotent: a marker file records a completed
 * extract, so re-runs are free.
 */
export async function ensureVSCode(cacheRoot: string): Promise<VSCodePaths> {
  const expected = pin.sha256[platform];
  if (!expected) throw new Error(`no sha256 pinned for ${platform}`);

  const dir = join(cacheRoot, `vscode-${pin.version}-${platform}`);
  const marker = join(dir, ".complete");
  const root = join(dir, "VSCode-linux-x64");
  const paths: VSCodePaths = {
    electron: join(root, "code"),
    cli: join(root, "bin", "code"),
    version: pin.version,
  };
  if (await exists(marker)) return paths;

  await rm(dir, { recursive: true, force: true });
  await mkdir(dir, { recursive: true });

  // Version-addressed stable URL; the archive for a released version is
  // immutable, which is what makes the hardcoded sha256 meaningful.
  const url = `https://update.code.visualstudio.com/${pin.version}/${platform}/stable`;
  // The archive lives inside the just-recreated cache dir, never a shared
  // tmp path: a fixed /tmp name would let a concurrent run truncate the
  // file between this run's verify and extract.
  const archive = join(dir, "download.tar.gz");
  console.log(`downloading VS Code ${pin.version} (${platform})`);
  await downloadVerified(url, expected, archive);

  execFileSync("tar", ["-xzf", archive, "-C", dir]);
  await rm(archive, { force: true });
  if (!(await exists(paths.electron))) {
    throw new Error(`extracted archive is missing ${paths.electron}`);
  }
  await writeFile(marker, `${pin.version}\n`);
  return paths;
}

/**
 * Install the extension .vsix into an isolated extensions dir via the code
 * CLI, so the launched window sees exactly one extension.
 */
export function installVsix(
  paths: VSCodePaths,
  vsix: string,
  extensionsDir: string,
  userDataDir: string,
): void {
  execFileSync(
    paths.cli,
    [
      "--install-extension",
      vsix,
      "--extensions-dir",
      extensionsDir,
      "--user-data-dir",
      userDataDir,
    ],
    { stdio: "inherit" },
  );
}
