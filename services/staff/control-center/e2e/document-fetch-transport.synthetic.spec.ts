import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import { extname, resolve } from "node:path";
import { expect, test } from "@playwright/test";

async function settlesWithin(promise: Promise<void>): Promise<boolean> {
  return Promise.race([
    promise.then(() => true),
    new Promise<false>((resolve) => setTimeout(() => resolve(false), 2_000)),
  ]);
}

test("synthetic: documentFetch сохраняет native transport до завершения body", async ({
  page,
}) => {
  const staticRoot = resolve("dist-synthetic");
  let receive!: (value: {
    method?: string;
    header?: string;
    body: string;
  }) => void;
  const received = new Promise<{
    method?: string;
    header?: string;
    body: string;
  }>((resolve) => {
    receive = resolve;
  });
  let confirmClose!: () => void;
  const closed = new Promise<void>((resolve) => {
    confirmClose = resolve;
  });
  let confirmBodyStarted!: () => void;
  const bodyStarted = new Promise<void>((resolve) => {
    confirmBodyStarted = resolve;
  });
  let confirmBodyClose!: () => void;
  const bodyClosed = new Promise<void>((resolve) => {
    confirmBodyClose = resolve;
  });
  let confirmCancelClose!: () => void;
  const cancelClosed = new Promise<void>((resolve) => {
    confirmCancelClose = resolve;
  });
  let confirmUnreadClose!: () => void;
  const unreadClosed = new Promise<void>((resolve) => {
    confirmUnreadClose = resolve;
  });
  const server = createServer(async (request, response) => {
    const address = new URL(request.url ?? "/", "http://127.0.0.1");
    if (address.pathname === "/pending") {
      const chunks: Buffer[] = [];
      request.on("data", (chunk: Buffer) => chunks.push(chunk));
      request.on("end", () => {
        receive({
          method: request.method,
          header:
            typeof request.headers["x-fixture"] === "string"
              ? request.headers["x-fixture"]
              : undefined,
          body: Buffer.concat(chunks).toString("utf8"),
        });
      });
      response.on("close", () => {
        if (!response.writableEnded) confirmClose();
      });
      return;
    }
    if (address.pathname === "/stream") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.write('{"partial":');
      confirmBodyStarted();
      response.on("close", () => {
        if (!response.writableEnded) confirmBodyClose();
      });
      return;
    }
    if (address.pathname === "/success") {
      response.writeHead(200, {
        "Content-Type": "text/plain",
        "X-Fixture": "preserved",
      });
      response.write("success-");
      setTimeout(() => response.end("body"), 10);
      return;
    }
    if (address.pathname === "/cancel-stream") {
      response.writeHead(200, { "Content-Type": "text/plain" });
      response.write("partial");
      response.on("close", () => {
        if (!response.writableEnded) confirmCancelClose();
      });
      return;
    }
    if (address.pathname === "/unread") {
      response.writeHead(200, { "Content-Type": "text/plain" });
      response.write("partial");
      response.on("close", () => {
        if (!response.writableEnded) confirmUnreadClose();
      });
      return;
    }
    const relative = address.pathname.replace(/^\/+/, "") || "index.html";
    const file = resolve(staticRoot, relative);
    if (!file.startsWith(`${staticRoot}/`)) {
      response.writeHead(404).end();
      return;
    }
    const contentType: Record<string, string> = {
      ".css": "text/css",
      ".html": "text/html; charset=utf-8",
      ".js": "text/javascript",
      ".woff2": "font/woff2",
    };
    try {
      const contents = await readFile(file);
      response.writeHead(200, {
        "Content-Type":
          contentType[extname(file)] ?? "application/octet-stream",
      });
      response.end(contents);
    } catch {
      response.writeHead(404).end();
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as AddressInfo).port;
  try {
    await page.goto(
      `http://127.0.0.1:${String(port)}/e2e/fixtures/document-fetch-transport.html`,
      { waitUntil: "domcontentloaded" },
    );
    await expect(page.locator("body")).toHaveAttribute("data-ready", "true");
    await page.getByRole("button", { name: "Начать запрос" }).click();
    await expect(page.locator("#outcome")).toHaveText("pending");
    await expect(received).resolves.toEqual({
      method: "POST",
      header: "preserved",
      body: "fixture-body",
    });
    await page.getByRole("button", { name: "Отменить запрос" }).click();
    await expect(page.locator("#outcome")).toHaveText("AbortError");
    await expect(closed).resolves.toBeUndefined();
    await page.getByRole("button", { name: "Начать чтение body" }).click();
    await expect(bodyStarted).resolves.toBeUndefined();
    await expect(page.locator("#body-outcome")).toHaveText("reading");
    await page.getByRole("button", { name: "Отменить чтение body" }).click();
    const serverClosedBeforeCleanup = await Promise.race([
      bodyClosed.then(() => true),
      new Promise<false>((resolve) => setTimeout(() => resolve(false), 500)),
    ]);
    expect({
      outcome: await page.locator("#body-outcome").textContent(),
      serverClosed: serverClosedBeforeCleanup,
    }).toEqual({ outcome: "AbortError", serverClosed: true });
    await page.getByRole("button", { name: "Прочитать полный ответ" }).click();
    await expect(page.locator("#success-outcome")).toHaveText(
      JSON.stringify({
        status: 200,
        url: "/success",
        redirected: false,
        type: "basic",
        header: "preserved",
        before: false,
        after: true,
        text: "success-body",
        clone: {
          status: 200,
          url: "/success",
          redirected: false,
          type: "basic",
          before: false,
          after: true,
          text: "success-body",
        },
        repeatName: "TypeError",
      }),
    );
    await page.getByRole("button", { name: "Отменить response body" }).click();
    await expect(page.locator("#cancel-stream-outcome")).toHaveText(
      "cancelled",
    );
    expect(await settlesWithin(cancelClosed)).toBe(true);
    await page
      .getByRole("button", { name: "Получить непрочитанный ответ" })
      .click();
    await expect(page.locator("#unread-outcome")).toHaveText("headers");
    await page.getByRole("button", { name: "Приостановить документ" }).click();
    expect(await settlesWithin(unreadClosed)).toBe(true);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
