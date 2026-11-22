import { expect, test } from "@playwright/test";
import type {
  KubernetesToolView,
  KubernetesCaptureReadback,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

for (const lost of [false, true])
  test(`Kubernetes capture repair before artifact lost=${String(lost)}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: lost ? 390 : 1440, height: 1000 });
    await installSyntheticCsrf(page.context(), "k");
    let current: KubernetesCaptureReadback = {
      view: {
        summary: {
          captureRef: "koc_synthetic",
          streamRef: "kst_synthetic",
          captureGeneration: 1,
          version: 5,
          state: "WAITING_OWNER",
          reasonCode: "CAPTURE_RETRY_EXHAUSTED",
        },
        invocationRef: "inv_synthetic",
        runRef: "run_catalog_1",
        nodeRef: "nod_synthetic",
        operation: "kubernetes.exec",
        kind: "NONE",
        attemptCount: 3,
        repairGeneration: 0,
        sealedAt: "2026-11-16T02:00:00Z",
        artifacts: [],
        cleanup: {
          version: 1,
          repairGeneration: 0,
          outstandingCount: 2,
          outstandingBytes: 30,
          exhaustedCount: 1,
          eligibleExhaustedCount: 1,
        },
      },
    };
    const tool: KubernetesToolView = {
      invocationRef: current.view.invocationRef,
      runRef: current.view.runRef,
      nodeRef: current.view.nodeRef,
      connectionRef: "int_synthetic",
      targetRef: "target_synthetic",
      targetRevision: 1,
      operation: "kubernetes.exec",
      state: "CANCELLED",
      version: 4,
      waitingForGate: false,
      safeReasonCode: "NONE",
      captureStatuses: [current.view.summary],
    };
    let writes = 0,
      reconciliations = 0,
      key = "",
      nativeWrites = 0,
      deniedTools = 0,
      cleanupWrites = 0,
      cleanupReads = 0,
      cleanupKey = "";
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (url.pathname.endsWith("/kubernetes-captures")) {
        expect(url.pathname).toBe(
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-captures",
        );
        await route.fulfill({
          json: { items: [current.view], nextPageToken: "" },
        });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-tools")) {
        if (lost) {
          deniedTools++;
          await route.fulfill({
            status: 403,
            json: {
              type: "about:blank",
              title: "Integration denied",
              status: 403,
              code: "FORBIDDEN",
            },
          });
        } else
          await route.fulfill({ json: { items: [tool], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname === "/api/v1/kubernetes-captures/koc_synthetic") {
        expect(request.method()).toBe("GET");
        if (url.searchParams.has("repairIdempotencyKey")) {
          reconciliations++;
          expect(url.searchParams.get("repairIdempotencyKey")).toBe(key);
        }
        if (url.searchParams.has("cleanupIdempotencyKey")) {
          cleanupReads++;
          expect(url.searchParams.get("cleanupIdempotencyKey")).toBe(
            cleanupKey,
          );
        }
        await route.fulfill({
          json: {
            view: current.view,
            ...(url.searchParams.has("repairIdempotencyKey")
              ? { repairReceipt: current.repairReceipt }
              : {}),
            ...(url.searchParams.has("cleanupIdempotencyKey")
              ? { cleanupReceipt: current.cleanupReceipt }
              : {}),
          },
        });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/kubernetes-captures/koc_synthetic/cleanup-repair"
      ) {
        cleanupWrites++;
        expect(request.method()).toBe("POST");
        expect(request.postDataJSON()).toEqual({});
        expect(request.headers()["if-match"]).toBe('"1"');
        cleanupKey = request.headers()["idempotency-key"] ?? "";
        expect(cleanupKey).toBeTruthy();
        current = {
          view: {
            ...current.view,
            cleanup: {
              version: 2,
              repairGeneration: 1,
              outstandingCount: 2,
              outstandingBytes: 30,
              exhaustedCount: 0,
              eligibleExhaustedCount: 0,
            },
          },
          cleanupReceipt: {
            captureRef: "koc_synthetic",
            version: 2,
            repairGeneration: 1,
            repairedCount: 1,
            remainingExhaustedCount: 0,
            acceptedAt: "2026-11-16T02:02:00Z",
          },
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
      if (url.pathname === "/api/v1/kubernetes-captures/koc_synthetic/repair") {
        writes++;
        expect(request.method()).toBe("POST");
        expect(request.postDataJSON()).toEqual({});
        expect(request.headers()["if-match"]).toBe('"5"');
        key = request.headers()["idempotency-key"] ?? "";
        expect(key).toBeTruthy();
        current = {
          view: {
            ...current.view,
            summary: {
              ...current.view.summary,
              version: 6,
              state: "READY",
              reasonCode: "",
            },
            repairGeneration: 1,
          },
          repairReceipt: {
            captureRef: "koc_synthetic",
            repairGeneration: 1,
            version: 6,
            attemptCount: 3,
            acceptedAt: "2026-11-16T02:01:00Z",
          },
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
      if (request.method() !== "GET") nativeWrites++;
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
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
    const panel = page.getByTestId("kubernetes-capture");
    await expect(
      panel.getByRole("heading", { name: "Сохранение результата" }),
    ).toBeVisible();
    await expect(panel.getByText("Всего попыток: 3")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Остановить вызов", exact: true }),
    ).toHaveCount(0);
    await panel
      .getByRole("button", { name: "Возобновить сохранение", exact: true })
      .click();
    await expect.poll(() => writes).toBe(1);
    await expect(
      panel.getByRole("button", {
        name: "Возобновить сохранение",
        exact: true,
      }),
    ).toHaveCount(0);
    if (lost) await expect.poll(() => reconciliations).toBe(1);
    else expect(reconciliations).toBe(0);
    await expect(panel.getByText("Всего попыток: 3")).toBeVisible();
    await panel
      .getByRole("button", {
        name: "Возобновить очистку временных данных",
        exact: true,
      })
      .click();
    await expect.poll(() => cleanupWrites).toBe(1);
    await expect(
      panel.getByRole("button", {
        name: "Возобновить очистку временных данных",
        exact: true,
      }),
    ).toHaveCount(0);
    await expect(
      panel.getByText(
        "Ожидают удаления: 2, 30 байт. Исчерпан цикл очистки: 0.",
      ),
    ).toBeVisible();
    await expect(
      panel.getByText(
        "Восстановлено записей: 1. Ещё требуют отдельного восстановления: 0.",
      ),
    ).toBeVisible();
    if (lost) await expect.poll(() => cleanupReads).toBe(1);
    else expect(cleanupReads).toBe(0);
    expect(nativeWrites).toBe(0);
    if (lost) expect(deniedTools).toBeGreaterThan(0);
    expect(failures).toEqual([]);
  });

for (const denied of [403, 404])
  test(`Kubernetes capture hides denied Refresh without Tool prop fallback ${String(denied)}`, async ({
    page,
  }) => {
    await installSyntheticCsrf(page.context(), "k");
    await page.setViewportSize({ width: 390, height: 1000 });
    const capture: KubernetesCaptureReadback = {
      view: {
        summary: {
          captureRef: "koc_synthetic",
          streamRef: "kst_synthetic",
          captureGeneration: 1,
          version: 5,
          state: "WAITING_OWNER",
          reasonCode: "CAPTURE_RETRY_EXHAUSTED",
        },
        invocationRef: "inv_synthetic",
        runRef: "run_catalog_1",
        nodeRef: "nod_synthetic",
        operation: "kubernetes.exec",
        kind: "NONE",
        attemptCount: 3,
        repairGeneration: 0,
        sealedAt: "2026-11-16T02:00:00Z",
        artifacts: [],
      },
    };
    const tool: KubernetesToolView = {
      invocationRef: capture.view.invocationRef,
      runRef: capture.view.runRef,
      nodeRef: capture.view.nodeRef,
      connectionRef: "int_synthetic",
      targetRef: "target_synthetic",
      targetRevision: 1,
      operation: "kubernetes.exec",
      state: "CANCELLED",
      version: 4,
      waitingForGate: false,
      safeReasonCode: "NONE",
      captureStatuses: [capture.view.summary],
    };
    let reads = 0,
      writes = 0;
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (url.pathname.endsWith("/kubernetes-captures")) {
        await route.fulfill({
          json: { items: [capture.view], nextPageToken: "" },
        });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-tools")) {
        await route.fulfill({ json: { items: [tool], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname === "/api/v1/kubernetes-captures/koc_synthetic") {
        reads++;
        if (reads === 1) await route.fulfill({ json: capture });
        else
          await route.fulfill({
            status: denied,
            json: {
              type: "about:blank",
              title: "Capture unavailable",
              status: denied,
              code: denied === 403 ? "FORBIDDEN" : "NOT_FOUND",
            },
          });
        return;
      }
      if (request.method() !== "GET") writes++;
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
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
    const panel = page.getByTestId("kubernetes-capture");
    await expect(panel.getByText("Всего попыток: 3")).toBeVisible();
    await expect(
      panel.getByRole("button", {
        name: "Возобновить сохранение",
        exact: true,
      }),
    ).toBeEnabled();
    await panel
      .getByRole("button", { name: "Обновить сохранение", exact: true })
      .click();
    await expect.poll(() => reads).toBe(2);
    await expect(panel.getByText("Всего попыток: 3")).toHaveCount(0);
    await expect(panel.locator(".status-badge")).toHaveCount(0);
    await expect(
      panel.getByRole("button", {
        name: "Возобновить сохранение",
        exact: true,
      }),
    ).toHaveCount(0);
    expect(writes).toBe(0);
    expect(failures).toEqual([]);
  });
