import { createServer, type ServerResponse } from "node:http";
import { expect, test, type Request } from "@playwright/test";
import { observeSyntheticAssetCancellation } from "./synthetic-asset-cancellation";

test("synthetic: поздняя image отменяется только вместе со своим document loader", async ({
  page,
  browserName,
}) => {
  test.skip(
    browserName !== "chromium",
    "CDP loader identity is Chromium-specific",
  );
  let documents = 0;
  let pendingDocument: ServerResponse | undefined;
  let pendingSignal: ServerResponse | undefined;
  let documentStarted: (() => void) | undefined;
  let imageStarted: (() => void) | undefined;
  const navigationStarted = new Promise<void>((resolve) => {
    documentStarted = resolve;
  });
  const pendingImage = new Promise<void>((resolve) => {
    imageStarted = resolve;
  });
  const html = "<!doctype html><title>Asset cancellation fixture</title>";
  const server = createServer((request, response) => {
    if (request.url === "/signal") {
      pendingSignal = response;
      return;
    }
    if (request.url === "/logo.png") {
      imageStarted?.();
      return;
    }
    if (request.url === "/") {
      documents += 1;
      if (documents === 2) {
        pendingDocument = response;
        documentStarted?.();
        return;
      }
    }
    response.writeHead(200, { "Content-Type": "text/html" });
    response.end(html);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture address unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  try {
    const assets = await observeSyntheticAssetCancellation(
      page,
      browserName,
      origin,
    );
    const failures: Request[] = [];
    page.on("requestfailed", (request) => {
      if (request.resourceType() === "image") failures.push(request);
    });
    await page.goto(origin);
    await page.evaluate(() => {
      void fetch("/signal").then(async (response) => {
        await response.text();
        const image = document.createElement("img");
        image.src = "/logo.png";
        document.body.append(image);
      });
    });
    await expect.poll(() => pendingSignal !== undefined).toBe(true);
    const navigation = page.goto(origin);
    // Ошибка навигации остаётся ошибкой проверки, но не unhandled rejection.
    const navigationResult = navigation.then(
      () => null,
      (error: unknown) => error,
    );
    await navigationStarted;
    // Запрос появляется ПОСЛЕ начала goto, но принадлежит ещё прежнему документу.
    if (!pendingSignal) throw new Error("Pending signal unavailable");
    pendingSignal.writeHead(200, { "Content-Type": "text/plain" });
    pendingSignal.end("ready");
    await pendingImage;
    if (!pendingDocument) throw new Error("Pending document unavailable");
    pendingDocument.writeHead(200, { "Content-Type": "text/html" });
    pendingDocument.end(html);
    expect(await navigationResult).toBeNull();
    await expect.poll(() => failures.length).toBe(1);
    const retiredImage = failures[0];
    if (!retiredImage) throw new Error("Image failure unavailable");
    expect(assets.confirmed(retiredImage), assets.describe(retiredImage)).toBe(
      true,
    );
    await page.route(`${origin}/logo.png`, (route) => route.abort("aborted"));
    await page.evaluate(() => {
      const image = document.createElement("img");
      image.src = "/logo.png";
      document.body.append(image);
    });
    await expect.poll(() => failures.length).toBe(2);
    const unrelated = failures[1];
    if (!unrelated) throw new Error("Independent failure unavailable");
    expect(unrelated.failure()?.errorText).toBe("net::ERR_ABORTED");
    expect(assets.confirmed(unrelated)).toBe(false);
  } finally {
    await page.close();
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
