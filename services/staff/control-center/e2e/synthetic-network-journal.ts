import { writeFile } from "node:fs/promises";
import type { Page, Request, TestInfo } from "@playwright/test";
import { syntheticFetchIDHeader } from "./synthetic-abort-observer";

// Только локальная synthetic оснастка: без headers/body/query и без классификации ошибок.
export function syntheticNetworkJournal(page: Page, testInfo: TestInfo) {
  const events: Array<Record<string, string | number | boolean>> = [];
  let overflow = false;
  const record = (
    event: string,
    fields: Record<string, string | number | boolean> = {},
  ) => {
    if (events.length >= 4096) {
      overflow = true;
      return;
    }
    events.push({
      sequence: events.length,
      timestampUTC: new Date().toISOString(),
      event,
      ...fields,
    });
  };
  const observe = (event: string) => (request: Request) => {
    const url = new URL(request.url());
    if (url.origin !== "https://kodex.test") return;
    if (
      event !== "requestfailed" &&
      !url.pathname.startsWith("/api/v1/") &&
      request.resourceType() !== "document"
    )
      return;
    record(event, {
      path: url.pathname,
      method: request.method(),
      resource: request.resourceType(),
      identity: request.headers()[syntheticFetchIDHeader] ?? "",
      ...(event === "requestfailed"
        ? { code: request.failure()?.errorText ?? "UNKNOWN" }
        : {}),
    });
  };
  page.on("request", observe("request"));
  page.on("requestfinished", observe("requestfinished"));
  page.on("requestfailed", observe("requestfailed"));
  page.on("framenavigated", (frame) => {
    if (frame === page.mainFrame())
      record("navigation-commit", { path: new URL(frame.url()).pathname });
  });
  page.on("console", (message) => {
    if (message.type() === "error")
      record("console-error", {
        accessControl: message.text().includes("due to access control checks"),
        path:
          /https:\/\/kodex\.test(\/api\/v1\/[a-z0-9/-]+)/.exec(
            message.text(),
          )?.[1] ?? "",
      });
  });
  return {
    record,
    async finish() {
      const path = testInfo.outputPath("synthetic-network-safe.json");
      await writeFile(
        path,
        JSON.stringify({ schemaVersion: 1, overflow, events }, null, 2),
        { flag: "wx", mode: 0o600 },
      );
      await testInfo.attach("synthetic-network-safe", {
        path,
        contentType: "application/json",
      });
      if (overflow) throw new Error("Synthetic network journal overflow");
    },
  };
}
