import { expect, test } from "@playwright/test";
import type { KubernetesToolView } from "../src/shared/api/generated/openapi/types.gen";
import { fileInputFixture } from "../src/shared/lib/kubernetes-file-input.test-support";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

for (const denied of [false, true]) {
  test(`Kubernetes FILE original metadata before terminal denied=${String(denied)}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: denied ? 390 : 1440, height: 1000 });
    await installSyntheticCsrf(page.context(), "k");
    const tool: KubernetesToolView = {
      invocationRef: "inv_synthetic",
      runRef: "run_catalog_1",
      nodeRef: "nod_synthetic",
      connectionRef: "int_synthetic",
      targetRef: "target_synthetic",
      targetRevision: 1,
      operation: "kubernetes.cp.upload",
      state: "RUNNING",
      version: 3,
      waitingForGate: false,
      safeReasonCode: "NONE",
      captureStatuses: [],
    };
    let source = fileInputFixture(),
      reject = false,
      writes = 0,
      reads = 0;
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname.endsWith("/kubernetes-tools")) {
        await route.fulfill({ json: { items: [tool], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-captures")) {
        expect(url.pathname).toBe(
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-captures",
        );
        expect(request.method()).toBe("GET");
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-file-inputs")) {
        expect(url.pathname).toBe(
          "/api/v1/integration-invocations/inv_synthetic/kubernetes-file-inputs",
        );
        expect(request.method()).toBe("GET");
        reads++;
        if (reject)
          await route.fulfill({
            status: 403,
            json: {
              type: "about:blank",
              status: 403,
              title: "Forbidden",
              code: "FORBIDDEN",
            },
          });
        else
          await route.fulfill({
            json: { projection: "SOURCE", items: [source] },
          });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        if (request.method() !== "GET") writes++;
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
    const panel = page.getByTestId("kubernetes-file-input");
    await expect(
      panel.getByText("Материал подготовлен", { exact: true }),
    ).toBeVisible();
    await expect(panel.getByText(/art_synthetic.*arv_synthetic/)).toBeVisible();
    expect(reads).toBeGreaterThan(0);
    if (denied) {
      reject = true;
      await panel
        .getByRole("button", { name: "Обновить источник", exact: true })
        .click();
      await expect(panel.getByText(/art_synthetic.*arv_synthetic/)).toHaveCount(
        0,
      );
      await expect(
        panel.getByText("Материал подготовлен", { exact: true }),
      ).toHaveCount(0);
      reject = false;
      await panel
        .getByRole("button", { name: "Обновить источник", exact: true })
        .click();
      await expect(
        panel.getByText(/art_synthetic.*arv_synthetic/),
      ).toBeVisible();
    } else {
      source = {
        ...source,
        state: "TRANSFERRED",
        version: 3,
        stagedBytes: source.normalizedSizeBytes,
        acknowledgedBytes: source.normalizedSizeBytes,
        eofAcknowledged: true,
      };
      await panel
        .getByRole("button", { name: "Обновить источник", exact: true })
        .click();
      await expect(
        panel.getByText("INPUT подтверждён", { exact: true }),
      ).toBeVisible();
      await expect(
        panel.getByText(/Это не подтверждает удалённое выполнение/),
      ).toBeVisible();
    }
    await expect(
      page
        .getByTestId("kubernetes-tools")
        .getByRole("button", { name: "Остановить вызов", exact: true }),
    ).toBeVisible();
    expect(writes).toBe(0);
    expect(failures).toEqual([]);
  });
}

for (const acknowledged of [false, true]) {
  test(`Kubernetes FILE cleanup-only hides revoked source and recovers one lost repair ACK=${String(acknowledged)}`, async ({
    page,
  }) => {
    await installSyntheticCsrf(page.context(), "k");
    const tool: KubernetesToolView = {
      invocationRef: "inv_synthetic",
      runRef: "run_catalog_1",
      nodeRef: "nod_synthetic",
      connectionRef: "int_synthetic",
      targetRef: "target_synthetic",
      targetRevision: 1,
      operation: "kubernetes.cp.upload",
      state: "UNKNOWN_OUTCOME",
      version: 3,
      waitingForGate: false,
      safeReasonCode: "NONE",
      captureStatuses: [],
    };
    const initial = {
      sourceRef: "kfs_synthetic",
      invocationRef: tool.invocationRef,
      cleanup: {
        version: 3,
        repairGeneration: 0,
        outstandingCount: 1,
        outstandingBytes: 2048,
        exhaustedCount: 1,
        eligibleExhaustedCount: 1,
      },
    };
    const repaired = {
      ...initial,
      cleanup: {
        ...initial.cleanup,
        version: 4,
        repairGeneration: 1,
        exhaustedCount: 0,
        eligibleExhaustedCount: 0,
      },
    };
    let key = "",
      posts = 0,
      keyReads = 0,
      repairAvailable = false,
      denyRead = false;
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname.endsWith("/kubernetes-tools")) {
        await route.fulfill({ json: { items: [tool], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-captures")) {
        expect(url.pathname).toBe(
          "/api/v1/runs/run_catalog_1/nodes/nod_synthetic/kubernetes-captures",
        );
        expect(request.method()).toBe("GET");
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.endsWith("/kubernetes-stream-inputs")) {
        await route.fulfill({ json: { items: [], nextPageToken: "" } });
        return;
      }
      if (url.pathname.includes("/kubernetes-file-inputs")) {
        expect(
          url.pathname.startsWith(
            "/api/v1/integration-invocations/inv_synthetic/",
          ),
        ).toBe(true);
        if (request.method() === "POST") {
          expect(url.pathname.endsWith("/kfs_synthetic/cleanup-repair")).toBe(
            true,
          );
          expect(request.postData()).toBe("{}");
          expect(request.headers()["if-match"]).toBe('"3"');
          key = request.headers()["idempotency-key"] ?? "";
          expect(key.length).toBeGreaterThan(7);
          posts++;
          await route.abort("connectionreset");
          return;
        }
        expect(request.method()).toBe("GET");
        if (url.searchParams.get("cleanupOnly") !== "true" || denyRead) {
          await route.fulfill({
            status: 403,
            json: {
              status: 403,
              title: "Forbidden",
              code: "FORBIDDEN",
              type: "about:blank",
            },
          });
          return;
        }
        if (url.pathname.endsWith("/kubernetes-file-inputs")) {
          await route.fulfill({
            json: {
              projection: "CLEANUP_ONLY",
              items: [
                repairAvailable ? { ...initial, repairAvailable } : initial,
              ],
            },
          });
          return;
        }
        expect(url.pathname.endsWith("/kfs_synthetic")).toBe(true);
        const originalKey = url.searchParams.get("cleanupIdempotencyKey");
        if (originalKey) {
          expect(originalKey).toBe(key);
          keyReads++;
        }
        await route.fulfill({
          json: {
            projection: "CLEANUP_ONLY",
            cleanupView: repairAvailable
              ? {
                  ...(acknowledged && originalKey ? repaired : initial),
                  repairAvailable,
                }
              : initial,
            ...(acknowledged && originalKey
              ? {
                  cleanupReceipt: {
                    sourceRef: initial.sourceRef,
                    version: 4,
                    repairGeneration: 1,
                    repairedCount: 1,
                    remainingExhaustedCount: 0,
                    acceptedAt: new Date().toISOString(),
                  },
                }
              : {}),
          },
        });
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
    const panel = page.getByTestId("kubernetes-file-input"),
      cleanup = panel.getByTestId("kubernetes-file-cleanup");
    await expect(cleanup.getByText(/Осталось объектов: 1/)).toBeVisible();
    await expect(
      panel.getByText(/art_synthetic|arv_synthetic|Материал подготовлен/),
    ).toHaveCount(0);
    await expect(
      cleanup.getByText("Ручное возобновление очистки пока недоступно."),
    ).toBeVisible();
    await expect(
      cleanup.getByRole("button", { name: "Возобновить очистку", exact: true }),
    ).toHaveCount(0);
    expect(posts).toBe(0);
    repairAvailable = true;
    await cleanup
      .getByRole("button", { name: "Проверить очистку", exact: true })
      .click();
    await cleanup
      .getByRole("button", { name: "Возобновить очистку", exact: true })
      .click();
    if (acknowledged)
      await expect(
        cleanup.getByText(/Новый цикл очистки принят/),
      ).toBeVisible();
    else {
      await expect(
        cleanup.getByText(/Ответ на запрос неизвестен/),
      ).toBeVisible();
      await expect(
        cleanup.getByRole("button", {
          name: "Возобновить очистку",
          exact: true,
        }),
      ).toBeDisabled();
    }
    expect(posts).toBe(1);
    expect(keyReads).toBe(1);
    denyRead = true;
    await cleanup
      .getByRole("button", { name: "Проверить очистку", exact: true })
      .click();
    await expect(cleanup.getByText(/Осталось объектов:/)).toHaveCount(0);
    await expect(
      cleanup.getByRole("button", { name: "Возобновить очистку", exact: true }),
    ).toHaveCount(0);
    expect(posts).toBe(1);
    expect(failures).toEqual([]);
  });
}
