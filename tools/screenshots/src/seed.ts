import { execFile } from "node:child_process";
import { appendFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// The deployable demo projections, deployed one by one: a whole-plan deploy
// would refuse everything because demo deliberately carries invalid
// projections (`broken` doesn't compile; `bistate-counter` uses bi-state
// under engine_version 2), and per-name deploys keep each exit code 0.
const deployable = [
  "order-count",
  "order-notifications",
  "event-counter",
  "quirks",
];

interface HistoryEntry {
  contentHash: string;
  kind: string;
}

/**
 * Build the deployed state the live-panel scenarios photograph, driving the
 * real CLI against the overlay workspace (default env = the harness DB):
 *
 * - every valid projection deployed (`broken` stays invalid, a red dot)
 * - order-count carries a history worth showing: create, a logic-change
 *   update, then a rollback to the original (the timeline's branch-back)
 * - event-counter edited locally after its deploy, so status and the deploy
 *   plan show drift (local ahead: one update among skips)
 */
export async function seedDeployState(
  repoRoot: string,
  workspace: string,
): Promise<void> {
  const gaffer = join(repoRoot, "cli", "gaffer");
  const run = async (...args: string[]): Promise<string> => {
    const { stdout } = await execFileAsync(gaffer, args, {
      cwd: workspace,
      env: {
        ...process.env,
        GAFFER_TELEMETRY_OPTOUT: "1",
        GAFFER_NO_UPDATE_CHECK: "1",
      },
    });
    return stdout;
  };

  for (const name of deployable) {
    console.log(`seeding: deploy ${name}`);
    await run("deploy", name, "--yes");
  }

  // A comment is a canonical-query change, so this deploys as an update
  // flagged "logic change" and mints a second content hash to roll back from.
  console.log("seeding: order-count logic-change update");
  await appendFile(
    join(workspace, "projections", "order-count.js"),
    "\n// track shipped totals next\n",
  );
  await run("deploy", "order-count", "--yes");

  console.log("seeding: order-count rollback");
  // --all, and the oldest deploy by kind: a re-used local DB accumulates
  // entries across runs, so neither the default --limit 100 nor a bare
  // .at(-1) is guaranteed to reach the original create. With ledger
  // metadata the create is kind "deploy"; "created" is the metadata-less
  // attribution of the same write.
  const history = JSON.parse(
    await run("history", "order-count", "--json", "--all"),
  ) as HistoryEntry[];
  const original = history.findLast(
    (e) => e.kind === "deploy" || e.kind === "created",
  );
  if (!original)
    throw new Error("order-count history has no create entry after seeding");
  await run(
    "rollback",
    "order-count",
    original.contentHash.slice(0, 8),
    "--yes",
  );

  console.log("seeding: event-counter local drift");
  await appendFile(
    join(workspace, "projections", "event-counter.js"),
    "\n// count per category next\n",
  );
}
