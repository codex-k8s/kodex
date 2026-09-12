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
    if (request.url === "/api/v1/bootstrap") {
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
      const controller = new AbortController();
      const response = await fetch("/api/v1/bootstrap", {
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
      // Последующий text сохраняет native ошибку locked body, не дублирует событие.
      const lockedText = await response.text().then(
        () => false,
        (error: unknown) => error instanceof TypeError,
      );
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
        firstByte: first.value?.length === 1,
        nativeAbort,
        lockedText,
        foreignPreserved,
      };
    });
    expect(result).toEqual({
      firstByte: true,
      nativeAbort: true,
      lockedText: true,
      foreignPreserved: true,
    });
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
