import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import {
  _electron,
  type ElectronApplication,
  type Page,
} from "playwright-core";

import type { VSCodePaths } from "./vscode.js";

export type Theme = "light" | "dark";

// The stock themes: a themed editor reads as branding the docs don't own,
// and defaults are what most readers' own VS Code looks like.
const themeNames: Record<Theme, string> = {
  light: "Default Light Modern",
  dark: "Default Dark Modern",
};

export interface LaunchOptions {
  vscode: VSCodePaths;
  userDataDir: string;
  extensionsDir: string;
  workspace: string;
  theme: Theme;
  /** Extra PATH entries, prepended - the in-tree gaffer CLI lives here. */
  pathPrepend: string[];
}

export interface Session {
  app: ElectronApplication;
  page: Page;
}

/**
 * Seed the user-data-dir settings before launch. The theme is baked here
 * rather than toggled in-session: a relaunch per theme is slower but
 * deterministic, and the settings file doubles as the reproducibility
 * manifest (telemetry off, update prompts off, stable typography).
 *
 * gaffer.command is application scope only (workspace settings are ignored
 * as a hostile-workspace defence), so the extension finds the CLI via PATH,
 * not via anything written into the fixture workspace.
 */
export async function writeUserSettings(
  userDataDir: string,
  theme: Theme,
): Promise<void> {
  const settings = {
    "workbench.colorTheme": themeNames[theme],
    "workbench.startupEditor": "none",
    // Chat/AI surfaces are noise in docs shots (and prompt for sign-in).
    "chat.disableAIFeatures": true,
    "telemetry.telemetryLevel": "off",
    "update.mode": "none",
    "extensions.autoUpdate": false,
    "gaffer.cliUpdateNotifications": false,
    "security.workspace.trust.enabled": false,
    "editor.minimap.enabled": false,
    "editor.fontSize": 13,
    "window.zoomLevel": 0,
  };
  const dir = join(userDataDir, "User");
  await mkdir(dir, { recursive: true });
  await writeFile(
    join(dir, "settings.json"),
    JSON.stringify(settings, null, "\t"),
  );
}

export async function launchVSCode(opts: LaunchOptions): Promise<Session> {
  await writeUserSettings(opts.userDataDir, opts.theme);
  const env: Record<string, string> = {
    ...(process.env as Record<string, string>),
    PATH: [...opts.pathPrepend, process.env.PATH].filter(Boolean).join(":"),
    // The tapes' env-hygiene block: no first-run telemetry disclosure, no
    // update notice, in any CLI the extension spawns.
    GAFFER_TELEMETRY_OPTOUT: "1",
    GAFFER_NO_UPDATE_CHECK: "1",
  };
  delete env.ELECTRON_RUN_AS_NODE;

  const app = await _electron.launch({
    executablePath: opts.vscode.electron,
    args: [
      // Chromium sandbox needs user namespaces containers often lack.
      "--no-sandbox",
      "--disable-gpu",
      "--disable-updates",
      "--skip-welcome",
      "--skip-release-notes",
      "--disable-workspace-trust",
      // No keyring daemon under xvfb; without this, SecretStorage
      // (context.secrets) hangs and any activate() awaiting it hangs too.
      "--password-store=basic",
      `--user-data-dir=${opts.userDataDir}`,
      `--extensions-dir=${opts.extensionsDir}`,
      opts.workspace,
    ],
    env,
  });
  const page = await app.firstWindow();
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.waitForSelector(".monaco-workbench", { timeout: 60_000 });
  // The workbench element lands before the explorer and keybinding service
  // are ready to take input; give the shell a beat.
  await page.waitForTimeout(3_000);
  // Disabling chat leaves its (now empty) secondary side bar behind. Only
  // a toggle command exists, so check the bar is actually open first.
  if (await page.locator(".part.auxiliarybar").isVisible()) {
    await runCommand(page, "View: Toggle Secondary Side Bar Visibility");
    await page.waitForTimeout(500);
  }
  return { app, page };
}

/**
 * Answer an already-open quick input: type the filter, wait until the
 * focused row is the one asked for, accept. Enter accepts whatever row is
 * focused, and a hidden or disabled command silently falls through to the
 * top fuzzy match - some other command - so a mismatch fails loudly
 * instead. The wait also covers live QuickPicks that open on a loading
 * placeholder row and repaint when their data lands.
 */
export async function answerQuickInput(
  page: Page,
  text: string,
): Promise<void> {
  await page.waitForSelector(".quick-input-widget .input", {
    state: "visible",
    timeout: 15_000,
  });
  await page.keyboard.type(text, { delay: 20 });
  const focused = page
    .locator(".quick-input-widget .monaco-list-row.focused")
    .first();
  try {
    await focused
      .filter({ hasText: new RegExp(escapeRegExp(text), "i") })
      .waitFor({ timeout: 10_000 });
  } catch {
    const label = (await focused.getAttribute("aria-label")) ?? "(no row)";
    await page.keyboard.press("Escape");
    throw new Error(
      `quick input for ${JSON.stringify(text)} focused ${JSON.stringify(label)} instead`,
    );
  }
  await page.keyboard.press("Enter");
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Press a quick-input chord and wait for the widget before typing, so keys
// can't land in whatever held focus when the chord was swallowed.
async function quickInput(
  page: Page,
  chord: string,
  text: string,
): Promise<void> {
  await page.keyboard.press(chord);
  await answerQuickInput(page, text);
}

/** Open a workspace file through quick open, the way a user would. */
export async function openFile(page: Page, name: string): Promise<void> {
  await quickInput(page, "Control+P", name);
  await page.waitForSelector(`.tab[aria-label*="${name}"]`, {
    timeout: 30_000,
  });
}

/**
 * Run a command through the command palette. The focused match must carry
 * the given text, so pass the command's full title, not a fragment.
 */
export async function runCommand(page: Page, command: string): Promise<void> {
  await quickInput(page, "Control+Shift+P", command);
}
