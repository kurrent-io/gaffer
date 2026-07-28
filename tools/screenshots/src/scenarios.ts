import type { Page } from "playwright-core";

import { openFile } from "./harness.js";

export interface Scenario {
  /** Output basename: emits <name>-light / <name>-dark pairs. */
  name: string;
  /** Whether the scenario needs the seeded KurrentDB (live panels do). */
  needsDatabase: boolean;
  /** Drive the window to the state the shot should show. */
  run(page: Page): Promise<void>;
}

export const scenarios: Scenario[] = [
  {
    name: "toml-lenses",
    needsDatabase: false,
    async run(page) {
      await openFile(page, "gaffer.toml");
      // The lenses appear once the extension has activated, spawned the
      // LSP, and discovered the CLI - the long pole on a cold start.
      await page.waitForSelector(".codelens-decoration", { timeout: 90_000 });
      // The deploy-status roll-up lens starts as "loading status..." and
      // settles once the environment answers (or refuses); don't shoot
      // mid-fetch.
      // String-form expression: it evaluates in the page, where document
      // exists; a typed closure here would drag the DOM lib into a node
      // package.
      await page.waitForFunction(
        "!document.body.textContent?.includes('loading status')",
        undefined,
        {
          timeout: 60_000,
        },
      );
      // Fonts, lens layout, and the status bar settle a beat later.
      await page.waitForTimeout(1_500);
    },
  },
];
