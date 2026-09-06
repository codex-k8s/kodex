import { expect, type Page } from "@playwright/test";
import type { EmailEffectReceipt } from "../../src/shared/api/generated/openapi/types.gen";
import { syntheticBrowserSession } from "./browser-session";

export async function installEmailOidc(
  page: Page,
  receipt: EmailEffectReceipt,
) {
  let elevated = false;
  let confirmations = 0;
  let version = 1;
  let generation = "11111111-1111-4111-8111-111111111111";
  const state = "s".repeat(43);
  const stages: string[] = [];
  const csrf = "e".repeat(43);
  const replacementCsrf = "r".repeat(43);
  const cookieHeaders = (value: string, session: string) => ({
    "set-cookie": `__Host-kodex-session=${session}; Secure; HttpOnly; SameSite=Strict; Path=/\n__Host-kodex-csrf=${value}; Secure; SameSite=Strict; Path=/`,
  });
  await page.context().route("https://identity.invalid/**", async (route) => {
    const url = new URL(route.request().url());
    expect(url.pathname).toBe("/authorize");
    stages.push(url.pathname);
    expect(url.searchParams.get("prompt")).toBe("login");
    expect(url.searchParams.get("max_age")).toBe("0");
    expect(url.searchParams.get("state")).toBe(state);
    const callback = `https://kodex.test/auth/callback?code=synthetic-code&state=${state}`;
    await route.fulfill({
      contentType: "text/html",
      body: `<script>location.replace(${JSON.stringify(callback)})</script>`,
    });
  });
  await page.route("**/api/v1/session/authorization", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().headers().authorization).toBeUndefined();
    expect(route.request().postDataJSON()).toEqual({
      freshAuthentication: true,
      purpose: {
        kind: "EMAIL_EFFECT_RECONCILIATION",
        receiptRef: receipt.ref,
        receiptVersion: receipt.version,
        receiptDigest: receipt.externalReceiptDigest,
      },
    });
    stages.push("/api/v1/session/authorization");
    await route.fulfill({
      json: {
        authorizationUrl: `https://identity.invalid/authorize?prompt=login&max_age=0&state=${state}`,
      },
    });
  });
  await page.route("**/api/v1/session/callback", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().headers().authorization).toBeUndefined();
    expect(route.request().postDataJSON()).toEqual({
      code: "synthetic-code",
      state,
    });
    stages.push("/api/v1/session/callback");
    confirmations++;
    elevated = true;
    generation = "22222222-2222-4222-8222-222222222222";
    await route.fulfill({
      json: syntheticBrowserSession({ generation, version }),
      headers: { ...cookieHeaders(csrf, "synthetic-elevated"), ETag: '"1"' },
    });
  });
  await page.route("**/api/v1/session", async (route) => {
    expect(["GET", "PUT"]).toContain(route.request().method());
    if (route.request().method() === "PUT") version++;
    await route.fulfill({
      json: syntheticBrowserSession({ generation, version }),
    });
  });
  return {
    stages,
    confirmations: () => confirmations,
    consume(headers: Record<string, string>) {
      expect(elevated).toBe(true);
      expect(headers["x-csrf-token"]).toBe(csrf);
      elevated = false;
      generation = "33333333-3333-4333-8333-333333333333";
      version++;
      return cookieHeaders(replacementCsrf, "synthetic-ordinary");
    },
    replacementCsrf,
  };
}
