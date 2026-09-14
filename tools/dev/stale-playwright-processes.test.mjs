import assert from "node:assert/strict";
import test from "node:test";

import { selectStaleTrees } from "./stale-playwright-processes.mjs";

const home = "/home/qa";

function processItem(overrides) {
  return {
    pid: 1,
    ppid: 0,
    sessionId: 1,
    startTicks: "100",
    ageSeconds: 7200,
    executable: "/usr/bin/node",
    argv: ["node", "/home/qa/probe.mjs"],
    cmdlineSHA256: "a".repeat(64),
    ...overrides,
  };
}

test("selectStaleTrees returns an orphan probe owning a stale Playwright browser", () => {
  const selected = selectStaleTrees(
    [
      processItem({ pid: 100, ppid: 1 }),
      processItem({
        pid: 110,
        ppid: 100,
        executable:
          "/home/qa/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell",
        argv: [
          "chrome-headless-shell",
          "--user-data-dir=/tmp/playwright_chromiumdev_profile-safe123",
        ],
      }),
      processItem({ pid: 111, ppid: 110, executable: "/usr/bin/chrome-helper" }),
    ],
    home,
    3600,
  );
  assert.deepEqual(selected.map((item) => item.root.pid), [100]);
  assert.deepEqual(selected[0].browserPIDs, [110]);
});

test("selectStaleTrees ignores a browser that still belongs to a live supervisor", () => {
  const selected = selectStaleTrees(
    [
      processItem({ pid: 100, ppid: 50 }),
      processItem({
        pid: 110,
        ppid: 100,
        executable:
          "/home/qa/.cache/ms-playwright/chromium-1228/chrome-headless-shell",
        argv: [
          "chrome-headless-shell",
          "--user-data-dir=/tmp/playwright_chromiumdev_profile-safe123",
        ],
      }),
    ],
    home,
    3600,
  );
  assert.deepEqual(selected, []);
});

test("selectStaleTrees ignores recent and foreign Chromium processes", () => {
  const selected = selectStaleTrees(
    [
      processItem({
        pid: 110,
        ppid: 1,
        ageSeconds: 60,
        executable:
          "/home/qa/.cache/ms-playwright/chromium-1228/chrome-headless-shell",
        argv: [
          "chrome-headless-shell",
          "--user-data-dir=/tmp/playwright_chromiumdev_profile-recent",
        ],
      }),
      processItem({
        pid: 120,
        ppid: 1,
        executable: "/usr/bin/chrome-headless-shell",
        argv: [
          "chrome-headless-shell",
          "--user-data-dir=/tmp/playwright_chromiumdev_profile-foreign",
        ],
      }),
    ],
    home,
    3600,
  );
  assert.deepEqual(selected, []);
});

test("selectStaleTrees accepts Chromium cmdline flattened after process-title rewrite", () => {
  const selected = selectStaleTrees(
    [
      processItem({ pid: 200, ppid: 1 }),
      processItem({
        pid: 210,
        ppid: 200,
        executable:
          "/home/qa/.cache/ms-playwright/chromium_headless_shell-1228/chrome-headless-shell",
        argv: [
          "chrome-headless-shell --headless --user-data-dir=/tmp/playwright_chromiumdev_profile-flat123 --remote-debugging-pipe",
        ],
      }),
    ],
    home,
    3600,
  );
  assert.deepEqual(selected.map((item) => item.root.pid), [200]);
  assert.deepEqual(selected[0].browserPIDs, [210]);
});
