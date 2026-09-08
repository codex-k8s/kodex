import { createServer, type ServerResponse } from "node:http";
import { expect, test, type Page } from "@playwright/test";
import { installReadNetworkObserver } from "./ui-read-network";
const content = (child = false) =>
  `<!doctype html><style>@font-face{font-family:Fixture;src:url('/fonts/pending.woff2')}body{font-family:Fixture}</style><p>Fixture</p>${child ? '<iframe src="/frame"></iframe>' : ""}<script>const controller = new AbortController(); window.cancelFixture = () => controller.abort(); fetch('/module-read', { cache:'no-store',signal:controller.signal }).catch(()=>{});</script>`;
async function fixture(
  page: Page,
  action: (data: {
    origin: string;
    pending: Array<{ path: string; response: ServerResponse }>;
    network: Awaited<ReturnType<typeof installReadNetworkObserver>>;
  }) => Promise<void>,
) {
  const pending: Array<{ path: string; response: ServerResponse }> = [];
  let rootDocuments = 0;
  const server = createServer((request, response) => {
    const path = request.url ?? "";
    if (path === "/broken") {
      request.socket.destroy();
      return;
    }
    if (path === "/fonts/pending.woff2" || path === "/module-read") {
      pending.push({ path, response });
      return;
    }
    response.setHeader("content-type", "text/html");
    response.end(
      (path === "/" && ++rootDocuments === 1) || path === "/new"
        ? content()
        : path === "/parent"
          ? '<!doctype html><iframe src="/frame"></iframe>'
          : path === "/frame"
            ? content()
            : "<!doctype html><p>Next</p>",
    );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("Fixture listener unavailable");
  const origin = `http://127.0.0.1:${String(address.port)}`;
  try {
    const network = await installReadNetworkObserver(page, origin);
    await action({ origin, pending, network });
  } finally {
    await page.close();
    pending.forEach((value) => value.response.destroy());
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
}
test("document proof: full navigation cancels exact old font and non-API fetch", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin, { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await page.goto(origin + "/next", { waitUntil: "domcontentloaded" });
    try {
      await expect
        .poll(() => network.snapshot())
        .toMatchObject({
          rawFailedRequests: 2,
          confirmedCancellations: 2,
          unexplainedFailures: 0,
        });
    } catch (error) {
      console.log(JSON.stringify({ documentProof: network.safeDiagnostics() }));
      throw error;
    }
    for (const value of network.safeDiagnostics().failures)
      expect(value).toMatchObject({
        nativeIdentityKnown: false,
        documentRequestObserved: true,
        documentNavigationIntent: true,
        documentNavigationCommitted: true,
        documentNavigationWindow: true,
        exactCancellation: true,
      });
  });
});
test("document proof: same-document goto cannot authorize an independent non-API abort", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin, { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    expect(
      await page.goto(origin + "/#anchor", { waitUntil: "domcontentloaded" }),
    ).toBeNull();
    await page.evaluate(() =>
      (window as unknown as { cancelFixture(): void }).cancelFixture(),
    );
    await expect
      .poll(() => network.snapshot())
      .toMatchObject({
        rawFailedRequests: 1,
        confirmedCancellations: 0,
        unexplainedFailures: 1,
      });
    expect(network.safeDiagnostics().failures[0]).toMatchObject({
      documentNavigationCommitted: false,
      exactCancellation: false,
    });
  });
});
test("document proof: failed navigation does not authorize previous requests", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin, { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await expect(
      page.goto(origin + "/broken", {
        waitUntil: "domcontentloaded",
        timeout: 5000,
      }),
    ).rejects.toThrow();
    expect(network.snapshot().confirmedCancellations).toBe(0);
    for (const value of network.safeDiagnostics().failures)
      expect(value.exactCancellation).toBe(false);
  });
});
test("document proof: foreign iframe requests do not inherit main frame navigation", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin + "/parent", { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await page.goto(origin + "/next", { waitUntil: "domcontentloaded" });
    await expect
      .poll(() => network.snapshot())
      .toMatchObject({
        rawFailedRequests: 2,
        confirmedCancellations: 0,
        unexplainedFailures: 2,
      });
    for (const value of network.safeDiagnostics().failures)
      expect(value.documentRequestObserved).toBe(false);
  });
});
test("document proof: new-document fetch cancellation is not attributed to the completed old navigation", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin + "/next", { waitUntil: "domcontentloaded" });
    await page.goto(origin + "/new", { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await page.evaluate(() =>
      (window as unknown as { cancelFixture(): void }).cancelFixture(),
    );
    await expect
      .poll(() => network.snapshot())
      .toMatchObject({
        rawFailedRequests: 1,
        confirmedCancellations: 0,
        unexplainedFailures: 1,
      });
    expect(network.safeDiagnostics().failures[0]).toMatchObject({
      documentNavigationIntent: false,
      exactCancellation: false,
    });
  });
});

test("document proof: successful reload commits a different document", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin, { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await page.reload({ waitUntil: "domcontentloaded" });
    await expect
      .poll(() => network.snapshot())
      .toMatchObject({
        rawFailedRequests: 2,
        confirmedCancellations: 2,
        unexplainedFailures: 0,
      });
  });
});
test("document proof: page close does not manufacture missing requestfailed events", async ({
  page,
}) => {
  await fixture(page, async ({ origin, pending, network }) => {
    await page.goto(origin, { waitUntil: "domcontentloaded" });
    await expect.poll(() => pending.length).toBe(2);
    await page.close();
    expect(page.isClosed()).toBe(true);
    expect(network.snapshot()).toMatchObject({
      rawFailedRequests: 0,
      confirmedCancellations: 0,
      unexplainedFailures: 0,
    });
  });
});
