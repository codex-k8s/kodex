import { EventEmitter } from "node:events";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { Page, TestInfo } from "@playwright/test";
import { expect, test } from "vitest";
import { syntheticNetworkJournal } from "./synthetic-network-journal";

test("журнал сохраняет API без query/headers/body, пропускает успешные assets и сохраняет их FAIL", async () => {
  const directory = await mkdtemp(join(tmpdir(), "k1315-"));
  try {
    const page = new EventEmitter();
    let attached = "";
    const info = {
      outputPath: (name: string) => join(directory, name),
      attach: (_name: string, value: { path: string }) => {
        attached = value.path;
        return Promise.resolve();
      },
    } as unknown as TestInfo;
    const journal = syntheticNetworkJournal(page as unknown as Page, info);
    const request = (path: string, resource: string) => ({
      url: () => `https://kodex.test${path}?query=private-marker`,
      resourceType: () => resource,
      method: () => "GET",
      headers: () => ({ authorization: "private-marker" }),
      failure: () => ({ errorText: "NS_BINDING_ABORTED" }),
    });
    page.emit("request", request("/assets/example.js", "script"));
    page.emit("requestfinished", request("/assets/example.js", "script"));
    page.emit("request", request("/api/v1/bootstrap", "fetch"));
    page.emit("requestfailed", request("/assets/example.js", "script"));
    await journal.finish();
    const content = await readFile(attached, "utf8");
    expect(content).not.toContain("private-marker");
    const parsed = JSON.parse(content) as {
      events: Array<{ event: string }>;
      overflow: boolean;
    };
    expect(parsed.events.map((event) => event.event)).toEqual([
      "request",
      "requestfailed",
    ]);
    expect(parsed.overflow).toBe(false);
    expect((await stat(attached)).mode & 0o777).toBe(0o600);
    await expect(journal.finish()).rejects.toThrow("EEXIST");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("переполнение сохраняет bounded artifact и закрыто отклоняет проверку", async () => {
  const directory = await mkdtemp(join(tmpdir(), "k1315-"));
  try {
    const journal = syntheticNetworkJournal(
      new EventEmitter() as unknown as Page,
      {
        outputPath: (name: string) => join(directory, name),
        attach: () => Promise.resolve(),
      } as unknown as TestInfo,
    );
    for (let i = 0; i < 4097; i++) journal.record("navigation-intent");
    await expect(journal.finish()).rejects.toThrow(
      "Synthetic network journal overflow",
    );
    const result = JSON.parse(
      await readFile(join(directory, "synthetic-network-safe.json"), "utf8"),
    ) as { events: unknown[]; overflow: boolean };
    expect(result.events).toHaveLength(4096);
    expect(result.overflow).toBe(true);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
