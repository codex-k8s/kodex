import { createServer, type ServerResponse } from "node:http";
import { expect, test } from "@playwright/test";
import { installReadNetworkObserver } from "./ui-read-network";

// Настоящий browser Request lifecycle, только собственный loopback server.
// Один fixture выполняется существующим synthetic config во всех трёх движках.
test("network checkpoint: long completed stream keeps abort proof and real failures", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const pending: ServerResponse[] = [];
  const server = createServer((request, response) => {
    if (request.url === "/api/v1/pending") {
      pending.push(response);
      return;
    }
    if (request.url === "/api/v1/broken") {
      request.socket.destroy();
      return;
    }
    response.setHeader("cache-control", "no-store");
    response.setHeader("content-type", "text/html");
    response.end("<!doctype html><p>Fixture</p>");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture listener unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  try {
    const network = await installReadNetworkObserver(page, origin);
    await page.goto(origin);
    await page.evaluate(() => {
      const controller = new AbortController();
      (window as unknown as { cancelFixture(): void }).cancelFixture = () =>
        controller.abort();
      void fetch("/api/v1/pending", { signal: controller.signal }).catch(
        () => undefined,
      );
    });
    await expect.poll(() => pending.length).toBe(1);
    await page.evaluate(() =>
      (window as unknown as { cancelFixture(): void }).cancelFixture(),
    );
    await expect.poll(() => network.snapshot().confirmedCancellations).toBe(1);
    const first = network.checkpoint();
    expect(first.diagnostics.failures[0]?.exactCancellation).toBe(true);
    let completed = 0;
    page.on("requestfinished", () => completed++);
    for (let batch = 0; batch < 17; batch++) {
      const before = completed;
      await page.evaluate(async () => {
        for (let i = 0; i < 256; i += 16)
          await Promise.all(
            Array.from({ length: 16 }, async () => {
              const response = await fetch("/api/v1/fixture-read");
              await response.text();
            }),
          );
      });
      await expect.poll(() => completed - before).toBeGreaterThanOrEqual(256);
      network.checkpoint();
      expect(network.snapshot()).toMatchObject({
        overflow: 0,
        unexplainedFailures: 0,
        confirmedCancellations: 1,
      });
      expect(network.snapshot().retainedRequests).toBeLessThan(8);
    }
    expect(network.snapshot().observedRequests).toBeGreaterThan(4096);
    await page.evaluate(async () => {
      await fetch("/api/v1/broken").catch(() => undefined);
    });
    await expect
      .poll(() => network.snapshot().unexplainedFailures)
      .toBeGreaterThan(0);
    const failed = network.checkpoint();
    expect(
      failed.diagnostics.failures.some((value) => !value.exactCancellation),
    ).toBe(true);
    expect(network.snapshot()).toMatchObject({
      overflow: 0,
      confirmedCancellations: 1,
    });
    expect(network.snapshot().unexplainedFailures).toBeGreaterThan(0);
  } finally {
    await page.close();
    for (const response of pending) response.destroy();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
