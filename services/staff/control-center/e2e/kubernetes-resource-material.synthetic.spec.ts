import { readFile } from "node:fs/promises";
import { expect, test, type Page } from "@playwright/test";
import type {
  KubernetesResourceMaterialAdmission,
  KubernetesResourceMaterialView,
  PersonalIntegrationConnection,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
const connection: PersonalIntegrationConnection = {
  connectionRef: "int_kube001",
  connectionVersion: 7,
  name: "Личный Kubernetes",
  definitionKey: "kubernetes-managed",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  credentialDelivery: {
    kind: "BROKER_IDENTITY",
    brokerPurpose: "KUBERNETES_TARGET",
  },
};
const view: KubernetesResourceMaterialView = {
  materialRef: "krs_fixture001",
  connectionRef: connection.connectionRef,
  connectionVersion: 7,
  identityRef: "kbi_fixture001",
  identityRevision: 2,
  targetRef: "target_fixture001",
  namespace: "team",
  name: "application",
  generateName: "",
  operation: "kubernetes.create",
  secretType: "Opaque",
  patchType: "",
  state: "PREPARED",
  expiresAt: "2099-11-15T12:00:00Z",
  version: 1,
  reasonCode: "",
  credentialPolicy: "INITIATOR_REQUIRED",
  bindingVersion: 3,
};
const admission: KubernetesResourceMaterialAdmission = {
  material: view,
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
async function fixture(page: Page, lost = false) {
  const calls: string[] = [],
    failures: string[] = [];
  let current = { ...view },
    key = "";
  await installSyntheticCsrf(page.context(), "k");
  page.on("pageerror", () => failures.push("pageerror"));
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname === "/api/v1/me/integration-connections") {
      await route.fulfill({ json: { items: [connection] } });
      return;
    }
    const path = `/api/v1/me/integration-connections/${connection.connectionRef}/kubernetes-resource-materials`;
    if (url.pathname === path && request.method() === "POST") {
      calls.push("BEGIN");
      key = request.headers()["idempotency-key"] ?? "";
      expect(request.postDataJSON()).toEqual({
        expectedVersion: 0,
        expectedConnectionVersion: 7,
        operation: "kubernetes.create",
        namespace: "team",
        name: "application",
        generateName: "",
        secretType: "Opaque",
        patchType: "",
        contentSHA256: "a".repeat(64),
        contentBytes: 128,
      });
      expect(request.headers()["x-csrf-token"]).toBe("k".repeat(43));
      if (lost) await route.abort("failed");
      else await route.fulfill({ status: 201, json: admission });
      return;
    }
    if (url.pathname === path && request.method() === "GET") {
      calls.push("GET");
      expect([...url.searchParams.keys()]).toEqual([
        lost ? "beginIdempotencyKey" : "materialRef",
      ]);
      expect(
        url.searchParams.get(lost ? "beginIdempotencyKey" : "materialRef"),
      ).toBe(lost ? key : view.materialRef);
      await route.fulfill({
        json: current,
        headers: { ETag: `"${String(current.version)}"` },
      });
      return;
    }
    if (url.pathname === `${path}/${view.materialRef}/cancel`) {
      calls.push("CANCEL");
      expect(request.method()).toBe("POST");
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
    if (
      url.pathname.startsWith("/api/") ||
      url.hostname === "broker.example.invalid"
    ) {
      failures.push("unexpected network");
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
  const panel = page.locator(".kubernetes-resource-material");
  await expect(
    panel.getByRole("heading", { name: "Защищённые данные Secret" }),
  ).toBeVisible();
  await panel.getByLabel("Namespace Secret", { exact: true }).fill("team");
  await panel.getByLabel("Имя Secret", { exact: true }).fill("application");
  await panel
    .getByLabel("SHA256 защищённого содержимого", { exact: true })
    .fill("a".repeat(64));
  await panel
    .getByLabel("Размер защищённого содержимого в байтах", { exact: true })
    .fill("128");
  return {
    panel,
    calls,
    failures,
    available: () => {
      current = {
        ...current,
        state: "AVAILABLE",
        version: 2,
        expiresAt: "2099-11-15T12:15:00Z",
      };
    },
  };
}
for (const width of [1440, 390])
  test(`Secret material admission stays private and AVAILABLE never means target READY ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    const { panel, calls, failures, available } = await fixture(page);
    await expect(panel.locator('input[type="file"],textarea')).toHaveCount(0);
    await panel
      .getByRole("button", { name: "Подготовить допуск загрузки", exact: true })
      .click();
    const downloadEvent = page.waitForEvent("download");
    await panel
      .getByRole("button", { name: "Скачать допуск для локального CLI" })
      .click();
    const download = await downloadEvent,
      path = await download.path();
    expect(path).not.toBeNull();
    expect(JSON.parse(await readFile(path, "utf8"))).toEqual(admission);
    expect(await page.locator("body").innerText()).not.toContain(
      admission.uploadPermit,
    );
    expect(
      await page.evaluate(() => JSON.stringify([localStorage, sessionStorage])),
    ).not.toContain(admission.uploadPermit);
    await expect(panel).toContainText(
      "python3 -I -B tools/dev/managed-kubernetes-resource-material.py upload",
    );
    available();
    await panel
      .getByRole("button", {
        name: "Прочитать состояние материала",
        exact: true,
      })
      .click();
    await expect(panel.getByRole("status")).toHaveText(
      "Защищённый материал подготовлен; изменение Secret требует отдельного допуска",
    );
    await expect(panel).not.toContainText("READY");
    await expect(
      panel.getByRole("button", { name: "Скачать допуск для локального CLI" }),
    ).toHaveCount(0);
    expect(calls).toEqual(["BEGIN", "GET"]);
    expect(failures).toEqual([]);
  });
test("Secret material lost Begin reconciles original key, no second write or permit recovery; Cancel uses exact artifact ETag", async ({
  page,
}) => {
  const { panel, calls, failures } = await fixture(page, true);
  await panel
    .getByRole("button", { name: "Подготовить допуск загрузки", exact: true })
    .click();
  await expect(panel).toContainText("Результат записи неизвестен");
  await expect(
    panel.getByRole("button", {
      name: "Подготовить допуск загрузки",
      exact: true,
    }),
  ).toBeDisabled();
  await panel
    .getByRole("button", { name: "Прочитать состояние материала", exact: true })
    .click();
  await expect(
    panel.getByRole("button", { name: "Скачать допуск для локального CLI" }),
  ).toHaveCount(0);
  await panel
    .getByRole("button", { name: "Отозвать будущую возможность использования" })
    .click();
  await expect(panel.getByRole("status")).toHaveText(
    "Будущее использование отозвано",
  );
  expect(calls).toEqual(["BEGIN", "GET", "CANCEL"]);
  expect(failures).toEqual([]);
});
