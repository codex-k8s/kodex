import { createServer, type ServerResponse } from "node:http";
import { expect, test, type Request } from "@playwright/test";
import {
  installSyntheticAbortObserver,
  syntheticFetchIDHeader,
} from "./synthetic-abort-observer";
import { SyntheticFetchCorrelator } from "./synthetic-fetch-correlator";
import { isConfirmedSyntheticCancellation } from "./synthetic-diagnostics";

// Два одновременных настоящих fetch одного URL; никакой связи по очереди/таймауту.
test("synthetic: concurrent bootstrap отменяет ровно свой request и сохраняет network failure", async ({
  page,
  browserName,
}) => {
  const pending: ServerResponse[] = [];
  const receipts: Array<{
    path: string;
    identity: string | string[] | undefined;
    original: string | string[] | undefined;
    cookie: boolean;
  }> = [];
  const server = createServer((request, response) => {
    receipts.push({
      path: request.url ?? "",
      identity: request.headers[syntheticFetchIDHeader],
      original: request.headers["x-original"],
      cookie: !!request.headers.cookie,
    });
    if (request.url === "/") {
      response
        .writeHead(200, { "content-type": "text/html" })
        .end("<!doctype html><title>Network fixture</title>");
    } else if (request.url === "/api/v1/bootstrap") pending.push(response);
    else if (request.url === "/api/v1/failure") request.socket.destroy();
    else response.writeHead(200).end("outside");
  });
  server.requestTimeout = 5000;
  const other = createServer((request, response) => {
    receipts.push({
      path: "foreign-origin",
      identity: request.headers[syntheticFetchIDHeader],
      original: undefined,
      cookie: !!request.headers.cookie,
    });
    response.writeHead(200).end("other");
  });
  const start = async (target: typeof server) => {
    await new Promise<void>((resolve, reject) => {
      target.once("error", reject);
      target.listen(0, "127.0.0.1", resolve);
    });
    const address = target.address();
    if (!address || typeof address === "string")
      throw new Error("Missing fixture listener");
    return `http://127.0.0.1:${String(address.port)}`;
  };
  try {
    const origin = await start(server),
      foreign = await start(other);
    await expect(
      installSyntheticAbortObserver(
        page,
        () => undefined,
        "https://control.kodex.works",
      ),
    ).rejects.toThrow("Synthetic fixture origin required");
    const observer = new SyntheticFetchCorrelator<Request>();
    const failed: Request[] = [];
    await installSyntheticAbortObserver(
      page,
      (event) => observer.observe(event),
      origin,
    );
    page.on("request", (request) =>
      observer.request(
        request,
        request.url(),
        request.headers()[syntheticFetchIDHeader],
      ),
    );
    page.on("requestfailed", (request) => failed.push(request));
    await page.goto(origin);
    await page.evaluate(() => {
      const controller = new AbortController();
      const first = fetch("/api/v1/bootstrap", {
        signal: controller.signal,
        credentials: "omit",
        cache: "no-store",
        headers: { "x-original": "kept" },
      })
        .then((response) => response.text())
        .catch((error: unknown) =>
          error instanceof DOMException ? error.name : "UNKNOWN",
        );
      const second = fetch(
        new Request("/api/v1/bootstrap", {
          credentials: "omit",
          cache: "no-store",
          headers: { "x-original": "kept" },
        }),
      ).then((response) => response.text());
      Object.assign(window, {
        fixtureCancel: () => controller.abort(),
        fixtureFirst: first,
        fixtureSecond: second,
      });
    });
    await expect.poll(() => pending.length).toBe(2);
    const inputs = receipts.filter(
      (receipt) => receipt.path === "/api/v1/bootstrap",
    );
    expect(new Set(inputs.map((receipt) => receipt.identity)).size).toBe(2);
    expect(
      inputs.every(
        (receipt) =>
          typeof receipt.identity === "string" &&
          receipt.original === "kept" &&
          !receipt.cookie,
      ),
    ).toBe(true);
    await page.evaluate(() =>
      (window as unknown as { fixtureCancel(): void }).fixtureCancel(),
    );
    await expect.poll(() => failed.length).toBe(1);
    const cancelled = failed[0];
    if (!cancelled) throw new Error("Missing cancelled request");
    await expect.poll(() => observer.cancelled(cancelled)).toBe(true);
    expect(
      isConfirmedSyntheticCancellation(
        browserName,
        cancelled.failure()?.errorText ?? "UNKNOWN",
        observer.cancelled(cancelled),
      ),
    ).toBe(true);
    for (const response of pending)
      if (!response.destroyed) response.writeHead(200).end("accepted");
    expect(
      await page.evaluate(
        () =>
          (window as unknown as { fixtureFirst: Promise<string> }).fixtureFirst,
      ),
    ).toBe("AbortError");
    expect(
      await page.evaluate(
        () =>
          (window as unknown as { fixtureSecond: Promise<string> })
            .fixtureSecond,
      ),
    ).toBe("accepted");
    await page.evaluate(async () => {
      await fetch("/api/v1/failure", { credentials: "omit" }).catch(
        () => undefined,
      );
    });
    await expect.poll(() => failed.length).toBe(2);
    const network = failed[1];
    if (!network) throw new Error("Missing network failure");
    expect(observer.cancelled(network)).toBe(false);
    expect(
      isConfirmedSyntheticCancellation(
        browserName,
        network.failure()?.errorText ?? "UNKNOWN",
        observer.cancelled(network),
      ),
    ).toBe(false);
    await page.evaluate(async (target) => {
      await fetch("/outside", { credentials: "omit" });
      await fetch(target, { mode: "no-cors", credentials: "omit" });
    }, foreign);
    expect(
      receipts
        .filter((receipt) =>
          ["/outside", "foreign-origin"].includes(receipt.path),
        )
        .every((receipt) => !receipt.identity && !receipt.cookie),
    ).toBe(true);
  } finally {
    for (const target of [server, other]) {
      target.closeAllConnections();
      await new Promise<void>((resolve, reject) =>
        target.close((error) => (error ? reject(error) : resolve())),
      );
    }
  }
});
