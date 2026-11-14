import { expect, test } from "@playwright/test";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "../src/shared/api/generated/openapi/types.gen";
import { installSyntheticCsrf } from "./fixtures/browser-session";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";
import { isRecord } from "../src/shared/lib/exact-object";

test("собственный GitHub: две страницы discovery, точный выбор и новая проверка после сохранения", async ({
  page,
}) => {
  const failures: string[] = [];
  let credential: PersonalIntegrationCredential = {
    connectionRef: "int_github001",
    connectionVersion: 7,
    bindingRef: "ipc_github001",
    version: 3,
    policy: "INITIATOR_REQUIRED",
    state: "READY",
    credentialConfigured: true,
    verifiedCapabilityKeys: ["github.identity.read"],
    deniedCapabilityKeys: [],
  };
  let connection: PersonalIntegrationConnection = {
    connectionRef: credential.connectionRef,
    connectionVersion: 7,
    name: "Собственный GitHub",
    definitionKey: "github",
    policy: "INITIATOR_REQUIRED",
    enabled: true,
    available: true,
    credential,
  };
  let pendingPage = 0;
  let discoveryCalls = 0;
  const updates: unknown[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await installSyntheticCsrf(page.context(), "g");
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname === "/api/v1/me/integration-connections") {
      await route.fulfill({ json: { items: [{ ...connection, credential }] } });
      return;
    }
    const personal = `/api/v1/me/integration-connections/${connection.connectionRef}/credential`;
    if (url.pathname === personal && request.method() === "GET") {
      if (pendingPage) {
        const cursor = pendingPage;
        pendingPage = 0;
        credential = {
          ...credential,
          version: credential.version + 1,
          state: "READY",
          githubDiscovery: {
            identityId: 11,
            login: "synthetic-user",
            page: cursor,
            nextPage: cursor === 1 ? 2 : 0,
            items: [
              {
                id: cursor === 1 ? 17 : 29,
                fullName:
                  cursor === 1
                    ? "personal/repository"
                    : "organization/repository",
                ownerKind: cursor === 1 ? "User" : "Organization",
                private: true,
                archived: false,
                canRead: true,
                canPush: false,
              },
            ],
          },
        };
      }
      await route.fulfill({ json: credential });
      return;
    }
    if (url.pathname === personal + "/test" && request.method() === "POST") {
      discoveryCalls++;
      const body: unknown = request.postDataJSON();
      expect(isRecord(body)).toBe(true);
      if (!isRecord(body))
        throw new Error("Synthetic discovery request is invalid");
      expect(body.expectedConnectionVersion).toBe(7);
      expect(body.githubDiscoveryPage).toBe(discoveryCalls);
      expect(request.headers()["if-match"]).toBe(
        `"${String(credential.version)}"`,
      );
      expect(request.headers()["x-csrf-token"]).toBe("g".repeat(43));
      expect(request.headers()["idempotency-key"]).toBeTruthy();
      pendingPage = discoveryCalls;
      credential = {
        ...credential,
        state: "CHECKING",
        version: credential.version + 1,
      };
      delete credential.githubDiscovery;
      await route.fulfill({ json: credential });
      return;
    }
    const owner = `/api/v1/integration-connections/${connection.connectionRef}`;
    if (url.pathname === owner && request.method() === "GET") {
      await route.fulfill({
        json: {
          ref: connection.connectionRef,
          name: connection.name,
          version: 7,
          nextActions: ["UPDATE"],
          publicConfiguration: { scope_kind: "ALL_ACCESSIBLE" },
        },
      });
      return;
    }
    if (url.pathname === owner && request.method() === "PATCH") {
      const body: unknown = request.postDataJSON();
      updates.push(body);
      expect(request.headers()["if-match"]).toBe('"7"');
      expect(request.headers()["x-csrf-token"]).toBe("g".repeat(43));
      expect(body).toEqual({
        name: connection.name,
        publicConfiguration: {
          scope_kind: "SELECTED",
          repositories:
            '[{"id":17,"full_name":"personal/repository"},{"id":29,"full_name":"organization/repository"}]',
        },
      });
      connection = { ...connection, connectionVersion: 8 };
      credential = {
        ...credential,
        connectionVersion: 8,
        state: "CHECK_REQUIRED",
        version: credential.version + 1,
        verifiedCapabilityKeys: [],
      };
      delete credential.githubDiscovery;
      await route.fulfill({
        json: { ref: connection.connectionRef, version: 8 },
      });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push("unexpected API " + url.pathname);
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
    page.getByRole("heading", { name: "Репозитории GitHub" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Найти доступные репозитории" })
    .click();
  await expect(
    page.getByText("Подтверждённый аккаунт: synthetic-user. Страница 1."),
  ).toBeVisible();
  await page
    .getByRole("radio", { name: "Только выбранные репозитории" })
    .check();
  await page.getByRole("checkbox", { name: /personal\/repository/ }).check();
  await page.getByRole("button", { name: "Следующая страница" }).click();
  await expect(
    page.getByText("Подтверждённый аккаунт: synthetic-user. Страница 2."),
  ).toBeVisible();
  await page
    .getByRole("checkbox", { name: /organization\/repository/ })
    .check();
  await expect(
    page.getByText(
      "Выбрано 2 из 100. Выбор сохраняется при переходе между страницами.",
    ),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Сохранить область подключения" })
    .click();
  await expect.poll(() => updates.length).toBe(1);
  expect(discoveryCalls).toBe(2);
  await expect(page.getByText(/Подтверждённый аккаунт:/)).toHaveCount(0);
  expect(failures).toEqual([]);
});

test("исторический GitHub personal contract не показывает неподдерживаемый scope discovery", async ({
  page,
}) => {
  const failures: string[] = [];
  await installSyntheticCsrf(page.context(), "h");
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (
      url.pathname === "/api/v1/me/integration-connections" &&
      route.request().method() === "GET"
    ) {
      const credential: PersonalIntegrationCredential = {
        connectionRef: "int_legacy001",
        connectionVersion: 7,
        bindingRef: "ipc_legacy001",
        version: 3,
        policy: "INITIATOR_REQUIRED",
        state: "READY",
        credentialConfigured: true,
        verifiedCapabilityKeys: ["github.repository.metadata.read"],
        deniedCapabilityKeys: [],
      };
      const connection: PersonalIntegrationConnection = {
        connectionRef: credential.connectionRef,
        connectionVersion: 7,
        name: "Исторический GitHub",
        definitionKey: "github",
        policy: "INITIATOR_REQUIRED",
        enabled: true,
        available: true,
        credential,
      };
      await route.fulfill({ json: { items: [connection] } });
      return;
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push("unexpected legacy API");
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
    page.getByRole("heading", { name: "Исторический GitHub" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Репозитории GitHub" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Найти доступные репозитории" }),
  ).toHaveCount(0);
  expect(failures).toEqual([]);
});
