import { createServer } from "node:http";
import { expect, test } from "@playwright/test";
import {
  installSyntheticAbortObserver,
  syntheticFetchIDHeader,
  type SyntheticFetchEvent,
} from "./synthetic-abort-observer";

test("synthetic: native reader abort учитывается без text и без двойного terminal", async ({
  page,
}) => {
  let serverRequestID: string | undefined;
  const server = createServer((request, response) => {
    if (["/api/v1/bootstrap", "/control"].includes(request.url ?? "")) {
      if (request.url === "/api/v1/bootstrap")
        serverRequestID = String(request.headers[syntheticFetchIDHeader]);
      response.writeHead(200, {
        "Content-Type": "text/plain",
      });
      response.write("x");
      return;
    }
    response.writeHead(200, { "Content-Type": "text/html" });
    response.end("<!doctype html><title>Native reader fixture</title>");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture address unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  const events: SyntheticFetchEvent[] = [];
  try {
    await installSyntheticAbortObserver(
      page,
      (event) => events.push(event),
      origin,
    );
    await page.goto(origin);
    const result = await page.evaluate(async () => {
      const readAndAbort = async (path: string) => {
        const controller = new AbortController();
        const response = await fetch(path, {
          signal: controller.signal,
        });
        const body = response.body;
        if (!body) throw new Error("Fixture body unavailable");
        const reader = body.getReader();
        const first = await reader.read();
        const pending = reader.read();
        controller.abort();
        const nativeAbort = await pending.then(
          () => false,
          (error: unknown) =>
            error instanceof DOMException && error.name === "AbortError",
        );
        // Браузеры выбирают разные native ошибки для уже отменённого locked body.
        const lockedText = await response.text().then(
          () => null,
          (error: unknown) =>
            error instanceof Error
              ? {
                  name: error.name,
                  tag: Object.prototype.toString.call(error),
                }
              : null,
        );
        return {
          firstByte: first.value?.length === 1,
          nativeAbort,
          lockedText,
        };
      };
      const observed = await readAndAbort("/api/v1/bootstrap");
      const control = await readAndAbort("/control");
      const foreign = new Error("Unrelated stream failure");
      const other = new ReadableStream({
        start(stream) {
          stream.error(foreign);
        },
      });
      const foreignPreserved = await other
        .getReader()
        .read()
        .then(
          () => false,
          (error: unknown) => error === foreign,
        );
      return {
        observed,
        control,
        foreignPreserved,
      };
    });
    expect(result.foreignPreserved).toBe(true);
    expect(result.observed.firstByte).toBe(true);
    expect(result.observed.nativeAbort).toBe(true);
    expect(result.observed.lockedText).not.toBeNull();
    expect(result.observed).toEqual(result.control);
    const start = events.find((event) => event.phase === "start");
    if (!start) throw new Error("Fixture start event unavailable");
    expect(start.id).toBe(serverRequestID);
    expect(start.signalGeneration).toBeGreaterThan(0);
    expect(events.filter((event) => event.phase === "body-error")).toEqual([
      {
        phase: "body-error",
        id: start.id,
        url: `${origin}/api/v1/bootstrap`,
        signalGeneration: start.signalGeneration,
        signalAborted: true,
        reasonClass: "AbortError",
      },
    ]);
  } finally {
    await page.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
