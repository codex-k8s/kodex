import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmod,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, test, vi } from "vitest";
import { persistSessionRenewalEvidence } from "./session-renewal-evidence";

const directories: string[] = [];
async function directory(): Promise<string> {
  const path = await mkdtemp(join(tmpdir(), "k1276-"));
  await chmod(path, 0o700);
  directories.push(path);
  return path;
}
afterEach(async () => {
  await Promise.all(
    directories
      .splice(0)
      .map((path) => rm(path, { recursive: true, force: true })),
  );
});

async function files(path: string): Promise<string[]> {
  const entries = await readdir(path, { withFileTypes: true });
  return (
    await Promise.all(
      entries.map(async (entry) =>
        entry.isDirectory()
          ? files(join(path, entry.name))
          : [join(path, entry.name)],
      ),
    )
  ).flat();
}

test("list reporter сохраняет JSON, mode0600 и exact digest после PASS и FAIL", async () => {
  const output = await directory();
  const controlCenter = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  const configPath = join(output, "playwright.config.cjs");
  await writeFile(
    configPath,
    `module.exports = ${JSON.stringify({ testDir: join(controlCenter, "e2e/fixtures"), testMatch: "session-evidence.fixture.ts", outputDir: join(output, "results"), workers: 1, retries: 0, reporter: "list", timeout: 10_000, use: { trace: "off", screenshot: "off", video: "off" } })};\n`,
    { mode: 0o600 },
  );
  const result = await new Promise<{ code: number | null; output: string }>(
    (resolveResult, reject) => {
      const child = spawn(
        process.execPath,
        [
          join(controlCenter, "node_modules/playwright/cli.js"),
          "test",
          "--config",
          configPath,
        ],
        {
          cwd: controlCenter,
          env: { PATH: process.env.PATH, LANG: "C.UTF-8", NO_COLOR: "1" },
          stdio: ["ignore", "pipe", "pipe"],
          timeout: 25_000,
        },
      );
      let text = "";
      child.stdout.on("data", (data: Buffer) => {
        text += data.toString();
      });
      child.stderr.on("data", (data: Buffer) => {
        text += data.toString();
      });
      child.on("error", reject);
      child.on("close", (code) => resolveResult({ code, output: text }));
    },
  );
  expect(result.code).toBe(1);
  expect(result.output).toContain("1 passed");
  expect(result.output).toContain("1 failed");
  expect(result.output).not.toContain("private-prompt-cookie-ticket-sentinel");
  const paths = (await files(join(output, "results"))).filter((path) =>
    path.endsWith("/session-renewal-safe-evidence.json"),
  );
  expect(paths).toHaveLength(2);
  const statuses: string[] = [];
  for (const path of paths) {
    const payload = await readFile(path, "utf8");
    const data = JSON.parse(payload) as {
      schemaVersion: number;
      requirement: string;
      status: string;
      windowSHA256: string;
    };
    expect(data.schemaVersion).toBe(1);
    expect(data.requirement).toBe("MVP-UI-11");
    expect(data.windowSHA256).toMatch(/^[a-f0-9]{64}$/);
    statuses.push(data.status);
    expect((await stat(path)).mode & 0o777).toBe(0o600);
    expect(await readFile(path.replace(/\.json$/, ".sha256"), "utf8")).toBe(
      `${createHash("sha256").update(payload).digest("hex")}\n`,
    );
    expect(payload).not.toContain("private-prompt-cookie-ticket-sentinel");
  }
  expect(statuses.sort()).toEqual(["FAIL", "PASS"]);
}, 30_000);

test("existing evidence не заменяется и oversized projection не записывается", async () => {
  const output = await directory();
  const attach = vi.fn().mockResolvedValue(undefined);
  const info = {
    outputPath: (...segments: string[]) => join(output, ...segments),
    attach,
  };
  await persistSessionRenewalEvidence(info, { status: "FAIL" });
  const original = await readFile(
    join(output, "session-renewal-safe-evidence.json"),
    "utf8",
  );
  await expect(
    persistSessionRenewalEvidence(info, { status: "PASS" }),
  ).rejects.toThrow();
  expect(
    await readFile(join(output, "session-renewal-safe-evidence.json"), "utf8"),
  ).toBe(original);
  expect(attach).toHaveBeenCalledTimes(1);
  const other = await directory();
  await expect(
    persistSessionRenewalEvidence(
      {
        ...info,
        outputPath: (...segments: string[]) => join(other, ...segments),
      },
      { value: "x".repeat(65_537) },
    ),
  ).rejects.toThrow("size limit");
  expect(await readdir(other)).toEqual([]);
});
