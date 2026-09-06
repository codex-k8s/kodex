import { expect, test, type Page } from "@playwright/test";
import type {
  ProviderAccount,
  ProviderAccountBlocker,
  ProviderAccountQueuedWorkCancellation,
} from "../src/shared/api/generated/openapi/types.gen";

async function install(page: Page, failedDeletion = false) {
  let account: ProviderAccount = {
    ref: "pacc_lifecycle",
    version: 7,
    definitionKey: "openai-codex",
    name: "Учётная запись проверки",
    externalAccountMasked: "fixture",
    state: "AUTHORIZED",
    enabled: true,
    ready: true,
    createdAt: "2026-09-06T00:00:00Z",
    updatedAt: "2026-09-06T00:00:00Z",
    authorization: {
      ref: "authorization_current",
      method: "DEVICE_CODE",
      state: "AUTHORIZED",
    },
    nextActions: ["REFRESH_AUTHORIZATION", "CONFIGURE_CREDENTIAL", "DELETE"],
  };
  if (failedDeletion)
    account = {
      ...account,
      state: "DELETING",
      enabled: false,
      ready: false,
      nextActions: [],
      deletion: {
        ref: "pdel_lifecycle",
        version: 2,
        state: "FAILED",
        pendingCleanup: 1,
        requestedAt: "2026-09-06T00:00:00Z",
        safeReason: "CREDENTIAL_CLEANUP_FAILED",
        blockers: [
          { kind: "AGENT", total: 0 },
          { kind: "PROVIDER_POOL", total: 0 },
          { kind: "AUTOMATION", total: 0 },
          { kind: "ACTIVE_TURN", total: 0 },
          { kind: "QUEUED_TURN", total: 0 },
          { kind: "WARM_RUNTIME", total: 0 },
        ],
      },
    };
  const items: ProviderAccountBlocker[] = [
    {
      kind: "AGENT",
      ref: "agent_blocker",
      version: 2,
      name: "Фиксированная политика",
      projectRef: "project_test",
      canCancel: false,
    },
    {
      kind: "PROVIDER_POOL",
      ref: "agent_pool",
      version: 3,
      name: "Взвешенная политика",
      projectRef: "project_test",
      canCancel: false,
    },
    {
      kind: "AUTOMATION",
      ref: "schedule_blocker",
      version: 4,
      name: "Автоматизация",
      projectRef: "project_test",
      canCancel: false,
    },
    {
      kind: "ACTIVE_TURN",
      ref: "run_active",
      version: 5,
      name: "Активное исполнение",
      projectRef: "project_test",
      canCancel: false,
    },
    {
      kind: "QUEUED_TURN",
      ref: "run_queued",
      version: 6,
      name: "Ожидающее исполнение",
      projectRef: "project_test",
      canCancel: true,
    },
    {
      kind: "WARM_RUNTIME",
      ref: "assistant_warm",
      version: 7,
      name: "Рабочая среда",
      canCancel: false,
    },
  ];
  const writes: {
    path: string;
    headers: Record<string, string>;
    body: unknown;
  }[] = [];
  const failures: string[] = [];
  let receipt: ProviderAccountQueuedWorkCancellation | undefined;
  let verificationReads = 0;
  await page.context().addCookies([
    {
      name: "__Host-kodex-csrf",
      value: "c".repeat(43),
      domain: "kodex.test",
      path: "/",
      secure: true,
      sameSite: "Strict",
    },
  ]);
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (url.origin !== "https://kodex.test") {
      failures.push("Unexpected origin");
      await route.abort();
      return;
    }
    if (url.pathname === "/config/runtime-config.json") {
      await route.fulfill({
        json: {
          revision: "0".repeat(64),
          environment: "synthetic",
          apiBaseUrl: "/",
          realtimeUrl: "/api/v1",
          requestTimeoutMs: 10000,
          oidc: {
            authority: "https://identity.invalid",
            clientId: "synthetic",
            redirectUri: "/auth/callback",
            postLogoutRedirectUri: "/",
            scope: "openid",
          },
        },
      });
      return;
    }
    if (url.pathname === "/api/v1/provider-definitions") {
      await route.fulfill({
        json: {
          items: [
            {
              key: "openai-codex",
              name: "OpenAI",
              description: "Fixture",
              authorizationMethods: ["DEVICE_CODE"],
              modelIds: [],
              defaultModelId: "",
              available: true,
              ready: true,
              readinessBlockers: [],
            },
          ],
        },
      });
      return;
    }
    if (url.pathname === "/api/v1/provider-accounts") {
      await route.fulfill({ json: { items: [account], nextActions: [] } });
      return;
    }
    const prefix = `/api/v1/provider-accounts/${account.ref}`;
    if (url.pathname === prefix && request.method() === "GET") {
      if (account.verification?.state === "PENDING") {
        verificationReads++;
        account = {
          ...account,
          version: account.version + 1,
          verification: {
            ...account.verification,
            state: "VERIFIED",
            completedAt: "2026-09-06T00:01:00Z",
            safeReason: "CREDENTIAL_REACHABILITY_VERIFIED",
          },
        };
      }
      await route.fulfill({ json: account });
      return;
    }
    if (url.pathname === `${prefix}/blockers`) {
      const kind = url.searchParams.get("kind"),
        query = url.searchParams.get("query") ?? "";
      const filtered = failedDeletion
        ? []
        : items.filter(
            (item) =>
              (!kind || kind === item.kind) &&
              item.name.includes(query) &&
              (!receipt || item.kind !== "QUEUED_TURN"),
          );
      await route.fulfill({
        json: {
          items: filtered,
          total: filtered.length,
          hiddenCount: failedDeletion ? 0 : 2,
          accountVersion: account.version,
          deletionIntentVersion: account.deletion?.version ?? 0,
          contextDigest: (receipt ? "b" : "a").repeat(64),
        },
      });
      return;
    }
    if (request.method() !== "GET" && url.pathname.startsWith(prefix)) {
      const headers = request.headers();
      writes.push({
        path: url.pathname,
        headers,
        body: request.postData()
          ? (request.postDataJSON() as unknown)
          : undefined,
      });
      if (
        !headers["idempotency-key"] ||
        headers["x-csrf-token"] !== "c".repeat(43)
      )
        failures.push("Missing mutation boundary");
      if (
        failedDeletion &&
        url.pathname === prefix &&
        request.method() === "DELETE"
      ) {
        if (writes.length === 1) {
          expect(headers["if-match"]).toBe('"8"');
          account = { ...account, version: 9 };
          await route.abort("failed");
          return;
        }
        if (writes.length === 2) {
          expect(headers["if-match"]).toBe('"8"');
          expect(headers["idempotency-key"]).toBe(
            writes[0]?.headers["idempotency-key"],
          );
          await route.fulfill({ json: account });
          return;
        }
        expect(writes).toHaveLength(3);
        expect(headers["if-match"]).toBe('"9"');
        expect(headers["idempotency-key"]).not.toBe(
          writes[0]?.headers["idempotency-key"],
        );
        if (!account.deletion) throw new Error("Deletion fixture is missing");
        account = {
          ...account,
          version: 10,
          state: "DELETED",
          nextActions: [],
          deletion: {
            ...account.deletion,
            version: 3,
            state: "DELETED",
            pendingCleanup: 0,
            safeReason: "ACCOUNT_DELETED",
            completedAt: "2026-09-06T00:00:03Z",
          },
        };
        await route.fulfill({ json: account });
        return;
      }
      if (url.pathname === `${prefix}/queued-work/cancellation`) {
        if (!receipt) {
          expect(headers["if-match"]).toBe('"7"');
          expect(request.postDataJSON()).toEqual({
            selectedRunRefs: ["run_queued"],
            blockersDigest: "a".repeat(64),
          });
          account = { ...account, version: 8 };
          receipt = {
            account,
            outcomes: [{ runRef: "run_queued", outcome: "CANCELLED" }],
          };
          await route.abort("failed");
          return;
        }
        expect(headers["idempotency-key"]).toBe(
          writes[0]?.headers["idempotency-key"],
        );
        expect(headers["if-match"]).toBe('"7"');
        expect(request.postDataJSON()).toEqual(writes[0]?.body);
        await route.fulfill({ json: receipt });
        return;
      }
      if (url.pathname === `${prefix}/device-authorization/verification`) {
        account = {
          ...account,
          version: account.version + 1,
          verification: {
            ref: "verification_current",
            accountVersion: account.version,
            credentialRevision: 4,
            state: "PENDING",
            scope: "CREDENTIALED_CATALOG_REACHABILITY",
            requestedAt: "2026-09-06T00:00:01Z",
            safeReason: "VERIFICATION_PENDING",
          },
        };
        await route.fulfill({ json: account });
        return;
      }
      if (url.pathname === `${prefix}/device-reauthorizations`) {
        account = {
          ...account,
          version: account.version + 1,
          state: "PENDING_AUTHORIZATION",
          ready: false,
          verification: undefined,
          authorization: {
            ref: "authorization_replacement",
            method: "DEVICE_CODE",
            state: "PENDING",
            userCode: "FIXTURE-CODE",
            verificationUri: "https://kodex.test/verification",
            expiresAt: new Date(Date.now() + 120000).toISOString(),
          },
          nextActions: ["REFRESH_AUTHORIZATION", "DELETE"],
        };
        await route.fulfill({ json: account });
        return;
      }
    }
    if (url.pathname.startsWith("/api/")) {
      failures.push(`Unexpected API ${request.method()} ${url.pathname}`);
      await route.fulfill({ status: 404, json: {} });
      return;
    }
    const response = await route.fetch({
      url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
    });
    await route.fulfill({ response });
  });
  return {
    writes,
    failures,
    verificationReads: () => verificationReads,
    allowDeletion: () => {
      account = { ...account, version: 8, nextActions: ["DELETE"] };
    },
  };
}

for (const width of [390, 2900]) {
  test(`synthetic: FAILED Delete, server action и отдельный UNKNOWN replay ${String(width)}px`, async ({
    page,
  }) => {
    const fixture = await install(page, true);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/fixtures/impact.html?kind=provider");
    await page
      .getByRole("button", { name: "Зависимости и удаление", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Зависимости и удаление",
      exact: true,
    });
    await expect(
      dialog.getByText("Очистка не завершилась", { exact: true }),
    ).toBeVisible();
    const requestDelete = dialog.getByRole("button", {
      name: "Запросить удаление",
      exact: true,
    });
    await expect(requestDelete).toHaveCount(0);
    expect(fixture.writes).toHaveLength(0);
    fixture.allowDeletion();
    await dialog
      .getByRole("button", { name: "Перечитать состояние", exact: true })
      .click();
    await expect(requestDelete).toBeEnabled();
    await requestDelete.click();
    await dialog
      .getByRole("button", { name: "Подтвердить команду", exact: true })
      .click();
    const retry = dialog.getByRole("button", {
      name: "Повторить исходную команду",
      exact: true,
    });
    await expect(retry).toBeVisible();
    await expect(requestDelete).toBeDisabled();
    await page.waitForTimeout(500);
    expect(fixture.writes).toHaveLength(1);
    await retry.click();
    await expect(retry).toHaveCount(0);
    await expect(requestDelete).toBeEnabled();
    expect(fixture.writes).toHaveLength(2);
    await dialog.screenshot({
      path: test.info().outputPath(`provider-failed-${String(width)}.png`),
    });
    await requestDelete.click();
    await dialog
      .getByRole("button", { name: "Подтвердить команду", exact: true })
      .click();
    await expect(
      dialog.getByText("Учётная запись удалена после очистки", { exact: true }),
    ).toBeVisible();
    await expect(requestDelete).toHaveCount(0);
    expect(fixture.writes).toHaveLength(3);
    expect(fixture.failures).toEqual([]);
  });
  test(`synthetic: provider queue selection и exact UNKNOWN recovery ${String(width)}px`, async ({
    page,
  }) => {
    const fixture = await install(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/fixtures/impact.html?kind=provider");
    await page
      .getByRole("button", { name: "Зависимости и удаление", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Зависимости и удаление",
      exact: true,
    });
    await expect(
      dialog.getByText(
        "Скрытых зависимостей: 2. Они также учитываются сервером при удалении.",
        { exact: true },
      ),
    ).toBeVisible();
    await expect(
      dialog.getByRole("link", { name: "Взвешенная политика", exact: true }),
    ).toHaveAttribute(
      "href",
      "/projects/project_test/agents/agent_pool?tab=runtime",
    );
    await expect(dialog.getByRole("checkbox")).toHaveCount(1);
    await dialog
      .getByRole("combobox", { name: "Вид зависимости", exact: true })
      .selectOption("QUEUED_TURN");
    await dialog.getByRole("checkbox").check();
    await dialog
      .getByRole("button", {
        name: "Отменить выбранные ожидающие Run: 1",
        exact: true,
      })
      .click();
    await dialog
      .getByRole("button", { name: "Подтвердить команду", exact: true })
      .click();
    const retry = dialog.getByRole("button", {
      name: "Повторить исходную команду",
      exact: true,
    });
    await expect(retry).toBeVisible();
    await page.waitForTimeout(500);
    expect(fixture.writes).toHaveLength(1);
    await retry.click();
    await expect(
      dialog.getByText("Выбранный Run 1: Отменён", { exact: true }),
    ).toBeVisible();
    expect(fixture.writes).toHaveLength(2);
    expect(fixture.failures).toEqual([]);
  });
  test(`synthetic: AUTHORIZED provider fresh verification и reauthorization ${String(width)}px`, async ({
    page,
  }) => {
    const fixture = await install(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/fixtures/impact.html?kind=provider");
    await page
      .getByRole("button", { name: "Проверить сейчас", exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: "Авторизация: Учётная запись проверки",
      exact: true,
    });
    await expect(
      dialog.getByText("Доступность каталога подтверждена", { exact: true }),
    ).toBeVisible({ timeout: 10000 });
    expect(fixture.verificationReads()).toBe(1);
    expect(fixture.writes).toHaveLength(1);
    await dialog
      .getByRole("button", { name: "Переавторизовать", exact: true })
      .click();
    await expect(
      dialog.getByText("FIXTURE-CODE", { exact: true }),
    ).toBeVisible();
    expect(fixture.writes[1]?.path).toMatch(/device-reauthorizations$/);
    expect(fixture.failures).toEqual([]);
  });
}
