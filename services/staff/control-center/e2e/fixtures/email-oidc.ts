import { createHash } from "node:crypto";
import { expect, type Page } from "@playwright/test";
import type { EmailEffectReceipt } from "../../src/shared/api/generated/openapi/types.gen";
export async function installEmailOidc(
  page: Page,
  receipt: EmailEffectReceipt,
) {
  let nonce = "";
  let challenge = "";
  let elevated = false;
  let confirmations = 0;
  const stages: string[] = [];
  const csrf = "e".repeat(43);
  const replacementCsrf = "r".repeat(43);
  const cookieHeaders = (value: string, session: string) => ({
    "set-cookie": `__Host-kodex-session=${session}; Secure; HttpOnly; SameSite=Strict; Path=/\n__Host-kodex-csrf=${value}; Secure; SameSite=Strict; Path=/`,
  });
  await page.context().route("https://identity.invalid/**", async (route) => {
    const url = new URL(route.request().url());
    stages.push(url.pathname);
    if (url.pathname === "/.well-known/openid-configuration") {
      await route.fulfill({
        json: {
          issuer: "https://identity.invalid",
          authorization_endpoint: "https://identity.invalid/authorize",
          token_endpoint: "https://identity.invalid/token",
          jwks_uri: "https://identity.invalid/jwks",
          response_types_supported: ["code"],
          subject_types_supported: ["public"],
          id_token_signing_alg_values_supported: ["RS256"],
        },
      });
      return;
    }
    if (url.pathname === "/authorize") {
      expect(url.searchParams.get("prompt")).toBe("login");
      expect(url.searchParams.get("max_age")).toBe("0");
      expect(url.searchParams.get("code_challenge_method")).toBe("S256");
      nonce = url.searchParams.get("nonce") ?? "";
      challenge = url.searchParams.get("code_challenge") ?? "";
      const redirect = url.searchParams.get("redirect_uri");
      expect(redirect).toBe("https://kodex.test/auth/callback");
      const callback = `${String(redirect)}?${new URLSearchParams({ code: "synthetic-code", state: url.searchParams.get("state") ?? "" }).toString()}`;
      await route.fulfill({
        contentType: "text/html",
        body: `<script>location.replace(${JSON.stringify(callback)})</script>`,
      });
      return;
    }
    if (url.pathname === "/token") {
      const body = new URLSearchParams(route.request().postData() ?? "");
      expect(body.get("code")).toBe("synthetic-code");
      expect(
        createHash("sha256")
          .update(body.get("code_verifier") ?? "")
          .digest("base64url"),
      ).toBe(challenge);
      const now = Math.floor(Date.now() / 1000);
      const encode = (value: unknown) =>
        Buffer.from(JSON.stringify(value)).toString("base64url");
      const token = `${encode({ alg: "RS256", typ: "JWT" })}.${encode({ iss: "https://identity.invalid", aud: "synthetic", sub: "subject_synthetic", exp: now + 3600, iat: now, auth_time: now, acr: "synthetic-owner", amr: ["pwd"], ...(nonce ? { nonce } : {}) })}.synthetic`;
      await route.fulfill({
        json: {
          access_token: "synthetic-email-access",
          id_token: token,
          token_type: "Bearer",
          expires_in: 3600,
          scope: "openid",
        },
      });
      return;
    }
    throw new Error("Unexpected synthetic OIDC endpoint");
  });
  await page.route("**/api/v1/session", async (route) => {
    if (route.request().method() !== "POST") {
      await route.fallback();
      return;
    }
    expect(route.request().postDataJSON()).toEqual({
      purpose: {
        kind: "EMAIL_EFFECT_RECONCILIATION",
        receiptRef: receipt.ref,
        receiptVersion: receipt.version,
        receiptDigest: receipt.externalReceiptDigest,
      },
    });
    expect(route.request().headers()["idempotency-key"]).toBeTruthy();
    expect(route.request().headers().authorization).toBe(
      "Bearer synthetic-email-access",
    );
    confirmations++;
    elevated = true;
    await route.fulfill({
      status: 204,
      headers: { ...cookieHeaders(csrf, "synthetic-elevated"), ETag: '"2"' },
    });
  });
  return {
    stages,
    confirmations: () => confirmations,
    consume(headers: Record<string, string>) {
      expect(elevated).toBe(true);
      expect(headers["x-csrf-token"]).toBe(csrf);
      elevated = false;
      return cookieHeaders(replacementCsrf, "synthetic-ordinary");
    },
    replacementCsrf,
  };
}
