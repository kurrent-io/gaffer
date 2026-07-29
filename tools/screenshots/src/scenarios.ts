import type { Locator, Page } from "playwright-core";

import { answerQuickInput, openFile, runCommand } from "./harness.js";

export interface Scenario {
  /** Output basename: emits <name>-light / <name>-dark pairs. */
  name: string;
  /** Whether the scenario needs the seeded KurrentDB (live panels do). */
  needsDatabase: boolean;
  /** Drive the window to the state the shot should show. */
  run(page: Page): Promise<void>;
}

// Webview panels render inside nested iframes; reach through both to wait on
// real content instead of sleeping and hoping. `last()` targets the newest
// webview, which is the tab a scenario just opened.
function webviewFrame(page: Page, text: string) {
  return page
    .frameLocator("iframe.webview")
    .last()
    .frameLocator("#active-frame")
    .getByText(text)
    .first();
}

// The deploy and history surfaces are deliberately absent from the command
// palette (`when: false`); their entry points are the CodeLens links, so the
// harness clicks them the way a user does. The regex is matched against the
// anchor's text with the codicon glyph stripped by \W.
function lens(page: Page, text: RegExp): Locator {
  return page.locator(".codelens-decoration a").filter({ hasText: text });
}

// Wait for the gaffer.toml lenses - the sign the extension, LSP, and CLI
// discovery are all up. The long pole on a cold start.
async function waitForLenses(page: Page): Promise<void> {
  await openFile(page, "gaffer.toml");
  await page.waitForSelector(".codelens-decoration", { timeout: 90_000 });
}

// Start a debug run by clicking order-count's happy-fixture Debug lens: a
// fixture lens carries the projection and fixture as arguments, so no picks
// follow. Exact-text "Debug" anchors appear per projection header then per
// fixture, in gaffer.toml block order; order-count is the first block, so
// index 0 is its header and 1 its happy fixture. Lens widgets are
// virtualised (off-screen lines have none), so the indexes can't be
// count-asserted - callers verify the outcome instead: the paused session
// must sit in order-count.js (expectActiveTab), so a reordered demo fails
// loudly rather than photographing the wrong projection.
async function clickHappyFixtureDebugLens(page: Page): Promise<void> {
  await lens(page, /^\W*Debug\W*$/)
    .nth(1)
    .click();
}

// Assert the active editor tab, the outcome-side check for positional lens
// clicks.
async function expectActiveTab(page: Page, name: string): Promise<void> {
  await page.waitForSelector(`.tab.active[aria-label*="${name}"]`, {
    timeout: 15_000,
  });
}

// Everything below the workbench chrome settles asynchronously (fonts, lens
// layout, webview paint); a short fixed beat before the shot keeps frames
// consistent without racing real waits.
const settle = (page: Page) => page.waitForTimeout(1_500);

// Order matters: debug-paused stays last. resetWindow doesn't clear its
// breakpoint or restore the Explorer side bar; only the end-of-theme
// app.close() does.
export const scenarios: Scenario[] = [
  {
    name: "toml-lenses",
    needsDatabase: false,
    async run(page) {
      await waitForLenses(page);
      // The deploy-status roll-up lens starts as "loading status..." and
      // settles once the environment answers (or refuses); don't shoot
      // mid-fetch. String-form expression: it evaluates in the page, where
      // document exists; a typed closure here would drag the DOM lib into a
      // node package.
      await page.waitForFunction(
        "!document.body.textContent?.includes('loading status')",
        undefined,
        { timeout: 60_000 },
      );
      await settle(page);
    },
  },
  {
    name: "deploy-plan",
    needsDatabase: true,
    async run(page) {
      await waitForLenses(page);
      // The status roll-up lens's Deploy link opens the plan tab.
      await lens(page, /^\W*Deploy\W*$/)
        .first()
        .click();
      // The seeded plan has updates (the drifted projections) among skips,
      // with the invalid projections refused.
      await webviewFrame(page, "event-counter").waitFor({ timeout: 60_000 });
      await settle(page);
    },
  },
  {
    name: "history-timeline",
    needsDatabase: true,
    async run(page) {
      await waitForLenses(page);
      // History lives in the Manage... actions pick; the first Manage lens
      // belongs to order-count (the first [[projection]] block), which
      // carries the seeded create -> update -> rollback timeline. The tab
      // title is the outcome-side check that the click hit order-count.
      await lens(page, /Manage/)
        .first()
        .click();
      await answerQuickInput(page, "History");
      await expectActiveTab(page, "History: order-count");
      await webviewFrame(page, "rollback").waitFor({ timeout: 60_000 });
      await settle(page);
    },
  },
  {
    name: "run-status",
    needsDatabase: false,
    async run(page) {
      await waitForLenses(page);
      // A fixture run to completion: the Gaffer panel's Status and State
      // views show the run summary and the projection's final state. With no
      // breakpoints the debug lens pauses at the first event, so continue
      // past it and let the fixture drain.
      await clickHappyFixtureDebugLens(page);
      // Continue only once the session is actually stopped (the paused-line
      // decoration): an F5 pressed while the session is still starting is
      // swallowed and the run stays paused forever.
      await page.waitForSelector(".debug-top-stack-frame-line", {
        timeout: 60_000,
      });
      await expectActiveTab(page, "order-count.js");
      await page.keyboard.press("F5");
      await page.waitForSelector(".debug-toolbar", {
        state: "hidden",
        timeout: 60_000,
      });
      await settle(page);
    },
  },
  {
    name: "debug-paused",
    needsDatabase: false,
    async run(page) {
      // A breakpoint inside the OrderPlaced handler, then debug against the
      // happy fixture: the shot is the editor paused on the handler line
      // with the debug side bar's variables in view.
      await openFile(page, "order-count.js");
      await runCommand(page, "Go to Line/Column...");
      await page.keyboard.type("7", { delay: 20 });
      await page.keyboard.press("Enter");
      await page.keyboard.press("F9");
      await waitForLenses(page);
      await clickHappyFixtureDebugLens(page);
      // Paused: the floating debug toolbar appears and the stopped line
      // gets the focused-stack-frame decoration.
      await page.waitForSelector(".debug-toolbar", { timeout: 60_000 });
      await page.waitForSelector(".debug-top-stack-frame-line", {
        timeout: 60_000,
      });
      await expectActiveTab(page, "order-count.js");
      // The variables side bar is the point of the shot; expand the Local
      // scope so state and event are visible, then park the mouse so no
      // hover tooltip photobombs the frame.
      await runCommand(page, "View: Show Run and Debug");
      await page
        .locator(".debug-pane .monaco-list-row", { hasText: "Local" })
        .first()
        .click();
      // Park over the activity bar edge: mid-frame coords hover editor
      // text, and the debug variable hover is exactly the photobomb being
      // avoided.
      await page.mouse.move(2, 400);
      await settle(page);
    },
  },
];
