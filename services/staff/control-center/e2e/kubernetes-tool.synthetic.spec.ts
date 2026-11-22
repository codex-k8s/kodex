import { expect, test } from "@playwright/test";
import type { KubernetesToolView } from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
for (const phase of ["WAITING_APPROVAL", "RUNNING"] as const)
  for (const lost of [false, true]) {
    test(`Kubernetes tool Stop before stream ${phase} lost=${String(lost)}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: lost ? 390 : 1440, height: 1000 });
      await installSyntheticCsrf(page.context(), "k");
      let current: KubernetesToolView = {
        invocationRef: "inv_synthetic",
        runRef: "run_catalog_1",
        nodeRef: "nod_synthetic",
        connectionRef: "int_synthetic",
        targetRef: "target_synthetic",
        targetRevision: 1,
        operation: "kubernetes.exec",
        state: phase,
        version: 3,
        waitingForGate: true,
        safeReasonCode: "NONE",
        captureStatuses: [],
      };
      let stops = 0,
        reads = 0;
      const failures: string[] = [];
      page.on("pageerror", (error) => failures.push(error.message));
      await page.route("**/*", async (route) => {
        const request = route.request(),
          url = new URL(request.url());
        if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
        if (url.pathname.endsWith("/kubernetes-tools")) {
          expect(url.pathname).toBe(
            "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-tools",
          );
          await route.fulfill({
            json: { items: [current], nextPageToken: "" },
          });
          return;
        }
        if (url.pathname.endsWith("/kubernetes-captures")) {
          await route.fulfill({ json: { items: [], nextPageToken: "" } });
          return;
        }
        if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
          await route.fulfill({ json: { items: [], nextPageToken: "" } });
          return;
        }
        if (
          url.pathname ===
          "/api/v1/integration-invocations/inv_synthetic/kubernetes-tool/stop"
        ) {
          stops++;
          expect(request.method()).toBe("POST");
          expect(request.postDataJSON()).toEqual({});
          expect(request.headers()["if-match"]).toBe('"3"');
          expect(request.headers()["idempotency-key"]).toBeTruthy();
          current = {
            ...current,
            state: "CANCELLED",
            version: 4,
            waitingForGate: false,
          };
          if (lost)
            await route.fulfill({
              status: 503,
              json: {
                type: "about:blank",
                title: "Unavailable",
                status: 503,
                code: "UNAVAILABLE",
              },
            });
          else await route.fulfill({ json: current });
          return;
        }
        if (
          url.pathname ===
          "/api/v1/integration-invocations/inv_synthetic/kubernetes-tool"
        ) {
          reads++;
          expect(request.method()).toBe("GET");
          await route.fulfill({ json: current });
          return;
        }
        if (url.pathname.startsWith("/api/")) {
          failures.push(`unexpected ${request.method()} API`);
          await route.abort();
          return;
        }
        await route.fulfill({
          response: await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          }),
        });
      });
      await page.goto("/e2e/fixtures/kubernetes-stream-input.html");
      const panel = page.getByTestId("kubernetes-tools");
      await expect(
        panel.getByText("Ждёт согласования", { exact: true }),
      ).toBeVisible();
      await expect(
        page
          .getByTestId("kubernetes-stream-input")
          .getByRole("button", { name: /Выбрать поток:/ }),
      ).toHaveCount(0);
      await panel
        .getByRole("button", { name: "Остановить вызов", exact: true })
        .click();
      await expect.poll(() => stops).toBe(1);
      await expect(
        panel.getByRole("button", { name: "Остановить вызов", exact: true }),
      ).toHaveCount(0);
      if (lost) await expect.poll(() => reads).toBe(1);
      else expect(reads).toBe(0);
      await expect(
        panel.getByText("Ждёт согласования", { exact: true }),
      ).toHaveCount(0);
      await expect(
        panel.getByText(/Закрытие вызова не подтверждает/),
      ).toBeVisible();
      expect(failures).toEqual([]);
    });
  }

// Проверяется отказ до создания stream: ToolPanel обнаруживает причину через прежний List, без stream selector и без нового effect.
test("Kubernetes ToolPanel explains the persisted PF preflight rejection without a stream", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 1000 });
  await installSyntheticCsrf(page.context(), "k");
  const view: KubernetesToolView = {
    invocationRef: "inv_synthetic",
    runRef: "run_catalog_1",
    nodeRef: "nod_synthetic",
    connectionRef: "int_synthetic",
    targetRef: "target_synthetic",
    targetRevision: 1,
    operation: "kubernetes.port-forward",
    state: "FAILED",
    version: 4,
    waitingForGate: false,
    safeReasonCode: "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED",
    captureStatuses: [],
  };
  let writes = 0;
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (url.pathname.endsWith("/kubernetes-tools")) {
      expect(request.method()).toBe("GET");
      await route.fulfill({ json: { items: [view], nextPageToken: "" } });
      return;
    }
    if (
      url.pathname ===
      "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-stream-inputs"
    ) {
      expect(request.method()).toBe("GET");
      await route.fulfill({ json: { items: [], nextPageToken: "" } });
      return;
    }
    if (url.pathname.endsWith("/kubernetes-captures")) {
      await route.fulfill({ json: { items: [], nextPageToken: "" } });
      return;
    }
    if (request.method() !== "GET") writes++;
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname.startsWith("/api/")) {
      failures.push(`unexpected ${request.method()} ${url.pathname}`);
      await route.abort();
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/kubernetes-stream-input.html");
  const panel = page.getByTestId("kubernetes-tools");
  await expect(panel.getByRole("status")).toContainText("PORT_FORWARD_SPDY_V1");
  await expect(
    panel.getByRole("button", { name: "Остановить вызов" }),
  ).toHaveCount(0);
  expect(writes).toBe(0);
  expect(failures).toEqual([]);
});
