import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { expect, test } from "@playwright/test";

test("synthetic: WebKit отменяет pending native documentFetch transport", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "webkit");
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
    const upstream = await fetch(
      `http://127.0.0.1:43122${address.pathname}${address.search}`,
    );
    const headers: Record<string, string> = {};
    upstream.headers.forEach((value, key) => {
      headers[key] = value;
    });
    response.writeHead(upstream.status, headers);
    response.end(Buffer.from(await upstream.arrayBuffer()));
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const port = (server.address() as AddressInfo).port;
  try {
    await page.goto(
      `http://127.0.0.1:${String(port)}/e2e/fixtures/document-fetch-transport.html`,
    );
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
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
