import { afterEach } from "vitest";
import { cleanup } from "@solidjs/testing-library";

// Tear down rendered Solid trees between tests (no vitest globals, so the
// library's auto-cleanup doesn't self-register).
afterEach(cleanup);
