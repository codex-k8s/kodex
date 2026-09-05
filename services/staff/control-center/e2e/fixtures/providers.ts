import type { Page } from "@playwright/test";
import type {
  ProviderAccount,
  ProviderDefinition,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function installProviderFixture(page: Page) {
  const events: string[] = [];
  const failures: string[] = [];
  const definition: ProviderDefinition = {
    key: "openai-codex",
    name: "OpenAI",
    description: "Synthetic provider",
    authorizationMethods: ["DEVICE_CODE", "API_KEY"],
    modelIds: [],
    defaultModelId: "",
    available: true,
    ready: true,
    readinessBlockers: [],
  };
  const now = new Date().toISOString();
  const accounts: ProviderAccount[] = [];
  await page.route("**/api/v1/provider-**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/provider-definitions") {
      await route.fulfill({ json: { items: [definition], nextPageToken: "" } });
      return;
    }
    if (url.pathname === "/api/v1/provider-accounts") {
      if (request.method() === "GET") {
        const query = url.searchParams.get("query") ?? "";
        await route.fulfill({
          json: {
            items: accounts.filter((item) => item.name.includes(query)),
            nextPageToken: "",
            nextActions: ["CREATE_CONNECTION"],
          },
        });
      } else {
        if (!request.headers()["idempotency-key"])
          failures.push("Missing create idempotency");
        const input = request.postDataJSON() as {
          name: string;
          definitionKey: "openai-codex";
        };
        const account: ProviderAccount = {
          ref: `pacc_synthetic_${String(accounts.length)}`,
          version: 1,
          definitionKey: input.definitionKey,
          name: input.name,
          externalAccountMasked: "",
          state: "PENDING_AUTHORIZATION",
          enabled: true,
          ready: false,
          nextActions: ["CONFIGURE_CREDENTIAL", "DELETE"],
          createdAt: now,
          updatedAt: now,
        };
        accounts.push(account);
        events.push("create");
        await route.fulfill({
          status: 201,
          json: account,
          headers: { ETag: '"1"' },
        });
      }
      return;
    }
    const ref = url.pathname.split("/")[4];
    const account = accounts.find((item) => item.ref === ref);
    if (!account) {
      await route.fallback();
      return;
    }
    if (request.method() !== "GET") {
      if (
        request.headers()["if-match"] !== `"${String(account.version)}"` ||
        !request.headers()["idempotency-key"]
      )
        failures.push("Invalid provider OCC headers");
      account.version += 1;
    }
    if (
      url.pathname.endsWith("/device-authorization") ||
      url.pathname.endsWith("/device-reauthorizations")
    ) {
      events.push(
        url.pathname.endsWith("/device-reauthorizations")
          ? "reauthorize"
          : "start",
      );
      account.state = "PENDING_AUTHORIZATION";
      account.ready = false;
      account.authorization = {
        ref: `pauth_synthetic_${String(account.version)}`,
        method: "DEVICE_CODE",
        state: "PENDING",
        userCode: "SYNTHETIC-CODE",
        verificationUri: "https://kodex.test/provider-verification",
        expiresAt: new Date(Date.now() + 120_000).toISOString(),
      };
      account.nextActions = ["REFRESH_AUTHORIZATION", "DELETE"];
    } else if (url.pathname.endsWith("/device-authorization/verification")) {
      events.push("verify");
      account.state = "AUTHORIZED";
      account.ready = true;
      if (account.authorization)
        account.authorization = {
          ref: account.authorization.ref,
          method: "DEVICE_CODE",
          state: "AUTHORIZED",
        };
      account.nextActions = [
        "REFRESH_AUTHORIZATION",
        "CONFIGURE_CREDENTIAL",
        "DELETE",
        "REVOKE",
      ];
    } else if (url.pathname.endsWith("/api-key-authorization")) {
      events.push("api-key");
      account.state = "AUTHORIZED";
      account.ready = true;
      account.authorization = {
        ref: `pauth_synthetic_${String(account.version)}`,
        method: "API_KEY",
        state: "AUTHORIZED",
      };
      account.nextActions = ["CONFIGURE_CREDENTIAL", "DELETE", "REVOKE"];
    } else if (
      request.method() === "DELETE" ||
      url.pathname.endsWith("/revocation")
    ) {
      events.push(request.method() === "DELETE" ? "delete" : "revoke");
      account.state = "REVOKED";
      account.enabled = false;
      account.ready = false;
      account.nextActions = [];
      delete account.authorization;
    }
    await route.fulfill({
      json: account,
      headers: { ETag: `"${String(account.version)}"` },
    });
  });
  return { events, failures };
}
