import { expect, test } from "@playwright/test";
import type {
  IntegrationConnection,
  TelegramSubscription,
  TelegramSubscriptionInput,
  InteractionDeliveryView,
} from "../src/shared/api/generated/openapi/types.gen";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

const connection: IntegrationConnection = {
  ref: "int_fixture01",
  version: 5,
  definitionKey: "telegram",
  credentialPolicy: "PROJECT_SHARED",
  name: "Сервисный бот проекта",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "Configured",
  capabilities: [],
  grants: [],
  nextActions: ["OPEN", "EDIT"],
  definitionVersion: "1.0.0",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
function subscription(version: number, enabled: boolean): TelegramSubscription {
  return {
    ref: "tgs_fixture01",
    projectRef: "prj_fixture01",
    connectionRef: connection.ref,
    version,
    enabled,
    canManage: true,
    state: enabled ? "ACTIVE" : "DISABLED",
    eventTypes: ["FINISHED", "FAILED", "HUMAN_GATE"],
    scopeDigest: "b".repeat(64),
    botId: 12345678,
    chatDisplay: "…1234",
    topicDisplay: "NONE",
    expiresAt: "2026-11-15T12:00:00Z",
    updatedAt: "2026-11-14T12:00:00Z",
    maxEffects: 25,
    usedEffects: 0,
  };
}
const unknown: InteractionDeliveryView = {
  ref: "idl_fixture01",
  version: 4,
  connectionRef: connection.ref,
  runRef: "run_fixture01",
  gateRef: "",
  eventType: "FINISHED",
  state: "UNKNOWN_OUTCOME",
  attempt: 1,
  retryable: false,
  safeErrorCode: "TELEGRAM_OUTCOME_UNKNOWN",
  createdAt: "2026-11-14T12:00:00Z",
  availableAt: "2026-11-14T12:00:00Z",
  completedAt: "2026-11-14T12:01:00Z",
  receiptRef: "",
};

for (const width of [1440, 390]) {
  for (const ambiguous of [false, true]) {
    test(`synthetic: Telegram project subscription ${String(width)}px ${ambiguous ? "unknown write" : "configure and revoke"}`, async ({
      page,
      context,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await context.addCookies([
        {
          name: "__Host-kodex-csrf",
          value: "c".repeat(43),
          url: "https://kodex.test",
          secure: true,
          sameSite: "Strict",
        },
      ]);
      const failures: string[] = [];
      const writes: { input: TelegramSubscriptionInput; version?: string }[] =
        [];
      let value: TelegramSubscription | undefined;
      page.on("pageerror", (error) => failures.push(error.message));
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
        if (
          url.pathname ===
          "/api/v1/projects/prj_fixture01/telegram-subscription"
        ) {
          if (route.request().method() === "GET") {
            await route.fulfill({
              json: {
                canManage: true,
                ...(value ? { subscription: value } : {}),
              },
            });
            return;
          }
          expect(route.request().method()).toBe("PUT");
          const input = route
            .request()
            .postDataJSON() as TelegramSubscriptionInput;
          writes.push({
            input,
            version: route.request().headers()["if-match"],
          });
          expect(route.request().headers()["idempotency-key"]).toBeTruthy();
          expect(route.request().headers()["x-csrf-token"]).toBe(
            "c".repeat(43),
          );
          value = subscription((value?.version ?? 0) + 1, input.enabled);
          if (ambiguous) {
            await route.abort("failed");
            return;
          }
          await route.fulfill({
            json: value,
            headers: {
              ETag: `"${String(value.version)}"`,
              "Cache-Control": "no-store",
            },
          });
          return;
        }
        if (url.pathname === "/api/v1/integration-connections") {
          expect(Object.fromEntries(url.searchParams)).toEqual({
            definitionKey: "telegram",
            pageSize: "20",
          });
          await route.fulfill({
            json: { items: [connection], nextPageToken: "" },
          });
          return;
        }
        if (
          url.pathname ===
          "/api/v1/projects/prj_fixture01/interaction-deliveries"
        ) {
          await route.fulfill({ json: { items: [unknown] } });
          return;
        }
        if (url.pathname.startsWith("/api/")) {
          failures.push("Unexpected API");
          await route.abort();
          return;
        }
        await route.fulfill({
          response: await route.fetch({
            url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
          }),
        });
      });
      await page.goto("/e2e/fixtures/telegram-notifications.html");
      await expect(
        page.getByText("Подписка не настроена.", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText("Исход неизвестен", { exact: true }),
      ).toBeVisible();
      await expect(
        page.getByText(
          "Доставка могла произойти. Автоматического повтора нет, чтобы не создать дубликат. Изменение настройки не отменяет прежнюю отправку.",
        ),
      ).toBeVisible();
      expect(writes).toHaveLength(0);
      await page
        .getByLabel("Сервисное подключение")
        .selectOption(connection.ref);
      await page.getByLabel("Новый срок, часов (1–168)").fill("48");
      await page.getByLabel("Лимит уведомлений (1–10000)").fill("25");
      await page
        .getByRole("button", { name: "Разрешить на указанный срок" })
        .click();
      await expect.poll(() => writes.length).toBe(1);
      expect(writes[0]).toEqual({
        input: {
          enabled: true,
          connectionRef: connection.ref,
          eventTypes: ["FINISHED", "FAILED", "HUMAN_GATE"],
          durationSeconds: 172800,
          maxEffects: 25,
        },
        version: undefined,
      });
      if (ambiguous) {
        await expect(
          page.getByText("Результат записи не подтверждён.", { exact: false }),
        ).toBeVisible();
        await expect(
          page.getByRole("button", { name: "Разрешить на указанный срок" }),
        ).toBeDisabled();
        await page
          .getByRole("button", { name: "Прочитать состояние заново" })
          .click();
        await expect(
          page.getByText("Бот 12345678, чат …1234, тема NONE", { exact: true }),
        ).toBeVisible();
        expect(writes).toHaveLength(1);
      } else {
        await expect(
          page.getByText("Настройка подтверждена сервером.", { exact: true }),
        ).toBeVisible();
        await page.getByRole("button", { name: "Отключить подписку" }).click();
        await expect(
          page.getByText("Отключена", { exact: true }),
        ).toBeVisible();
        expect(writes[1]).toEqual({
          input: { enabled: false },
          version: '"1"',
        });
      }
      await expect(page.locator("body")).not.toContainText(
        "telegramNotifications.",
      );
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await page
        .getByRole("button", { name: "Сменить synthetic owner" })
        .click();
      await expect(
        page.getByText("Бот 12345678, чат …1234, тема NONE", { exact: true }),
      ).toHaveCount(0);
      expect(failures).toEqual([]);
    });
  }
}
