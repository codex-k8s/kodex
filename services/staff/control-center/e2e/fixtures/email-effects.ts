import { expect, type Page } from "@playwright/test";
import { installEmailOidc } from "./email-oidc";
import type {
  EmailEffectReceiptView,
  EmailReconciliationInput,
  IntegrationConnection,
} from "../../src/shared/api/generated/openapi/types.gen";
export async function checkEmailEffects(
  page: Page,
  capture: () => Promise<void>,
): Promise<void> {
  const connection: IntegrationConnection = {
    ref: "email_synthetic",
    version: 5,
    definitionKey: "email",
    name: "Email synthetic",
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "",
    capabilities: [],
    grants: [],
    nextActions: [],
    definitionVersion: "1",
    definitionDigest: "a".repeat(64),
    publicConfiguration: {},
  };
  const view: EmailEffectReceiptView = {
    receipt: {
      ref: "receipt_synthetic",
      version: 7,
      invocationRef: "invocation_synthetic",
      externalReceiptDigest: "b".repeat(64),
      semanticInputDigest: "c".repeat(64),
      outcome: "UNKNOWN_OUTCOME",
      mailboxRef: "mailbox_synthetic",
      configurationRevision: 4,
      connectionRef: connection.ref,
      projectRef: "project_synthetic",
      createdAt: "2026-09-05T00:00:00Z",
      updatedAt: "2026-09-05T00:00:00Z",
    },
  };
  let decisions = 0;
  const oidc = await installEmailOidc(page, view.receipt);
  await page.route("**/api/v1/integration-connections**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/integration-connections") {
      await route.fulfill({
        json: { items: [connection], nextActions: [], nextPageToken: "" },
      });
      return;
    }
    if (path === `/api/v1/integration-connections/${connection.ref}`) {
      await route.fulfill({ json: connection });
      return;
    }
    await route.fallback();
  });
  await page.route(
    "**/api/v1/integration-invocations/invocation_synthetic/email-effect-receipt",
    (route) => route.fulfill({ headers: { ETag: '"7"' }, json: view }),
  );
  await page.route(
    "**/api/v1/email-effect-receipts/receipt_synthetic/reconciliation",
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"7"');
      expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      const body = route.request().postDataJSON() as EmailReconciliationInput;
      expect(body).toEqual({
        expectedReceiptDigest: "b".repeat(64),
        outcome: "NO_EFFECT_CONFIRMED",
        note: "Synthetic owner decision",
      });
      decisions++;
      view.decision = {
        ref: "decision_synthetic",
        version: 1,
        receiptRef: view.receipt.ref,
        receiptVersion: 7,
        receiptDigest: view.receipt.externalReceiptDigest,
        invocationRef: view.receipt.invocationRef,
        outcome: body.outcome,
        actorRef: "subject_synthetic",
        createdAt: "2026-09-05T00:01:00Z",
        expiresAt: "2026-09-05T00:02:00Z",
      };
      await route.fulfill({
        headers: { ...oidc.consume(route.request().headers()), ETag: '"1"' },
        json: view.decision,
      });
    },
  );
  await page.goto("/integrations");
  await page
    .getByRole("button", { name: "Сведения о подключении", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: connection.name,
    exact: true,
  });
  await dialog
    .getByLabel("Идентификатор операции", { exact: true })
    .fill(view.receipt.invocationRef);
  await dialog
    .getByRole("button", { name: "Найти квитанцию", exact: true })
    .click();
  await expect(
    dialog.getByText("Исход неизвестен", { exact: true }),
  ).toBeVisible();
  await expect(dialog.getByRole("combobox")).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "Подтвердить через OIDC", exact: true })
    .click();
  await expect
    .poll(() => ({
      confirmations: oidc.confirmations(),
      stages: oidc.stages,
      pathname: new URL(page.url()).pathname,
    }))
    .toEqual({
      confirmations: 1,
      stages: [
        "/.well-known/openid-configuration",
        "/authorize",
        "/.well-known/openid-configuration",
        "/token",
      ],
      pathname: "/integrations",
    });
  await expect(
    dialog.getByText("Исход неизвестен", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Зафиксировать решение", exact: true }),
  ).toBeDisabled();
  expect(decisions).toBe(0);
  await dialog.getByRole("combobox").selectOption("NO_EFFECT_CONFIRMED");
  await dialog
    .getByLabel("Примечание", { exact: true })
    .fill("Synthetic owner decision");
  await dialog
    .getByRole("button", { name: "Зафиксировать решение", exact: true })
    .click();
  expect(decisions).toBe(0);
  await page
    .getByRole("dialog", { name: "Решение владельца", exact: true })
    .getByRole("button", { name: "Зафиксировать решение", exact: true })
    .click();
  await expect(
    dialog.getByText("decision_synthetic / v1", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByText("Исход неизвестен", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Зафиксировать решение", exact: true }),
  ).toHaveCount(0);
  expect(decisions).toBe(1);
  expect(await page.evaluate(() => document.cookie)).toContain(
    `__Host-kodex-csrf=${oidc.replacementCsrf}`,
  );
  expect(
    await page.evaluate(() =>
      sessionStorage.getItem("kodex.email.reconciliation-attempts"),
    ),
  ).toBeNull();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.evaluate(() => window.scrollTo(0, 0));
  await capture();
  await dialog
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
}
