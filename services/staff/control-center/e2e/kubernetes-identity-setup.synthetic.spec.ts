import { readFile } from "node:fs/promises";
import { expect, test, type Page } from "@playwright/test";
import type {
  KubernetesIdentitySetupAdmission,
  KubernetesIdentitySetupView,
  PersonalIntegrationConnection,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

const connection: PersonalIntegrationConnection = {
  connectionRef: "int_kube001",
  connectionVersion: 7,
  name: "Личный Kubernetes",
  definitionKey: "kubernetes",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  kubernetesSetupRequirements: {
    targetRef: "ktg_fixture001",
    targetRevision: 2,
    canonicalContext: "operator-context",
    connectionVersion: 7,
    credentialPolicy: "INITIATOR_REQUIRED",
  },
  credentialDelivery: {
    kind: "BROKER_IDENTITY",
    brokerPurpose: "KUBERNETES_TARGET",
  },
};
const view: KubernetesIdentitySetupView = {
  setupRef: "ksu_fixture001",
  connectionRef: connection.connectionRef,
  connectionVersion: 7,
  bindingGeneration: 1,
  bindingVersion: 1,
  identityRef: "kid_fixture001",
  identityRevision: 1,
  canonicalContext: "operator-context",
  targetRef: "ktg_fixture001",
  state: "PREPARED",
  expiresAt: "2099-11-15T12:00:00Z",
  reasonCode: "",
  version: 1,
  credentialPolicy: "INITIATOR_REQUIRED",
};
const admission: KubernetesIdentitySetupAdmission = {
  setup: view,
  reconcileOnly: false,
  uploadPermit: Buffer.from("p".repeat(32)).toString("base64"),
  endpoint: {
    serverOrigin: "https://broker.example.invalid:9443",
    tlsServerName: "broker.example.invalid",
    certificateAuthorityPem: Buffer.from("synthetic-ca").toString("base64"),
    certificateAuthoritySHA256: "c".repeat(64),
    addressIPs: ["192.0.2.10"],
  },
};

async function fixture(page: Page, lostBegin = false, requirements = true) {
  const failures: string[] = [],
    calls: string[] = [];
  let key = "",
    current = { ...view };
  await installSyntheticCsrf(page.context(), "k");
  page.on("pageerror", () => failures.push("pageerror"));
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname === "/api/v1/me/integration-connections") {
      await route.fulfill({
        json: {
          items: [
            {
              ...connection,
              kubernetesSetupRequirements: requirements
                ? connection.kubernetesSetupRequirements
                : undefined,
            },
          ],
        },
      });
      return;
    }
    const path = `/api/v1/me/integration-connections/${connection.connectionRef}/kubernetes-setups`;
    if (url.pathname === path && request.method() === "POST") {
      calls.push("BEGIN");
      key = request.headers()["idempotency-key"] ?? "";
      expect(key).toBeTruthy();
      expect(request.headers()["x-csrf-token"]).toBe("k".repeat(43));
      expect(request.postDataJSON()).toEqual({
        expectedVersion: 0,
        expectedConnectionVersion: 7,
        contentSHA256: "a".repeat(64),
        contentBytes: 128,
      });
      if (lostBegin) await route.abort("failed");
      else await route.fulfill({ status: 201, json: admission });
      return;
    }
    if (url.pathname === path && request.method() === "GET") {
      calls.push("GET");
      expect([...url.searchParams.keys()]).toEqual([
        lostBegin ? "beginIdempotencyKey" : "setupRef",
      ]);
      expect(
        url.searchParams.get(lostBegin ? "beginIdempotencyKey" : "setupRef"),
      ).toBe(lostBegin ? key : view.setupRef);
      await route.fulfill({
        json: current,
        headers: { ETag: `"${String(current.version)}"` },
      });
      return;
    }
    if (
      url.pathname === `${path}/${view.setupRef}/cancel` &&
      request.method() === "POST"
    ) {
      calls.push("CANCEL");
      expect(request.headers()["if-match"]).toBe(
        `"${String(current.version)}"`,
      );
      expect(request.postDataJSON()).toEqual({});
      current = {
        ...current,
        state: "CANCELLED",
        version: current.version + 1,
      };
      await route.fulfill({
        json: current,
        headers: { ETag: `"${String(current.version)}"` },
      });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push("unexpected API");
      await route.abort();
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/personal-connections.html");
  await expect(
    page.getByRole("heading", { name: "Личное подключение Kubernetes" }),
  ).toBeVisible();
  if (requirements) {
    await page.getByLabel("SHA256 файла").fill("a".repeat(64));
    await page.getByLabel("Размер файла в байтах").fill("128");
  }
  return {
    calls,
    failures,
    complete() {
      current = {
        ...current,
        state: "COMPLETED",
        version: current.version + 1,
      };
    },
  };
}

for (const width of [1440, 390])
  test(`Kubernetes local setup: private admission download, exact CLI and TEST_REQUIRED ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 950 });
    const f = await fixture(page);
    expect(await page.locator('input[type="file"]').count()).toBe(0);
    await expect(
      page.getByText(
        "python3 -I -B tools/dev/managed-kubernetes-identity.py inspect --kubeconfig <private-export-file>",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      page.getByText(
        "Каноническое имя context этого подключения: operator-context",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      page.getByText(
        "python3 -I -B tools/dev/managed-kubernetes-identity.py export --kubectl <absolute-kubectl-binary> --kubeconfig <local-source-file> --context <selected-local-context> --target-context 'operator-context' --output <new-private-export-file>",
        { exact: true },
      ),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Подготовить загрузку", exact: true })
      .click();
    const downloadEvent = page.waitForEvent("download");
    await page.getByRole("button", { name: "Скачать допуск для CLI" }).click();
    const download = await downloadEvent;
    expect(JSON.parse(await readFile(await download.path(), "utf8"))).toEqual(
      admission,
    );
    expect(await page.locator("body").innerText()).not.toContain(
      admission.uploadPermit,
    );
    await expect(
      page.getByText("chmod 600 <admission-file>", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        "python3 -I -B tools/dev/managed-kubernetes-identity.py upload --admission-file <admission-file> --kubeconfig <private-export-file>",
        { exact: true },
      ),
    ).toBeVisible();
    f.complete();
    await page
      .getByRole("button", { name: "Прочитать состояние", exact: true })
      .click();
    await expect(
      page.getByText("Требуется проверка подключения", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Скачать допуск для CLI" }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Обновить подключение для проверки" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Подготовить загрузку", exact: true }),
    ).toBeDisabled();
    expect(f.calls).toEqual(["BEGIN", "GET"]);
    expect(f.failures).toEqual([]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBe(true);
  });

test("Kubernetes unknown Begin: original-key Get, no second Begin/permit recovery, Cancel uses setup ETag", async ({
  page,
}) => {
  const f = await fixture(page, true);
  await page
    .getByRole("button", { name: "Подготовить загрузку", exact: true })
    .click();
  await expect(
    page.getByText("Результат записи не подтверждён.", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Подготовить загрузку", exact: true }),
  ).toBeDisabled();
  await expect(page.getByLabel("Ключ или ссылка")).toBeDisabled();
  await page
    .getByRole("button", { name: "Прочитать состояние", exact: true })
    .click();
  await expect(
    page.getByText("Ожидает локальной загрузки", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Скачать допуск для CLI" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Отменить настройку", exact: true })
    .click();
  await expect(
    page.getByText("Настройка отменена", { exact: true }),
  ).toBeVisible();
  expect(f.calls).toEqual(["BEGIN", "GET", "CANCEL"]);
  expect(f.failures).toEqual([]);
});

test("Kubernetes missing export requirements block Begin without inventing context", async ({
  page,
}) => {
  const f = await fixture(page, false, false);
  await expect(
    page.getByText(
      "Требования локального экспорта недоступны или изменились.",
      { exact: false },
    ),
  ).toBeVisible();
  await expect(page.getByLabel("SHA256 файла")).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Подготовить загрузку", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByText("--target-context", { exact: false }),
  ).toHaveCount(0);
  expect(f.calls).toEqual([]);
  expect(f.failures).toEqual([]);
});
