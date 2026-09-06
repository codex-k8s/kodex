import { expect, type Page } from "@playwright/test";
import type {
  IntegrationConnection,
  InteractionIdentity,
  InteractionIdentityBindInput,
} from "../../src/shared/api/generated/openapi/types.gen";
export async function checkInteractionIdentities(page: Page): Promise<void> {
  const connection: IntegrationConnection = {
    ref: "connection_identity_synthetic",
    version: 17,
    definitionKey: "mattermost",
    name: "Mattermost synthetic",
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
  let identity: InteractionIdentity | undefined;
  let creates = 0;
  let revokes = 0;
  const queries: string[] = [];
  await page.route("**/api/v1/integration-connections**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/integration-connections") {
      expect(route.request().method()).toBe("GET");
      const query = new URL(route.request().url()).searchParams;
      const search = query.get("query") ?? "";
      queries.push(search);
      const more = query.get("pageToken") === "connections_next";
      await route.fulfill({
        json: {
          items: search
            ? []
            : [
                more
                  ? {
                      ...connection,
                      ref: "connection_second",
                      name: "Mattermost second",
                    }
                  : connection,
              ],
          nextActions: [],
          nextPageToken: !search && !more ? "connections_next" : "",
        },
      });
      return;
    }
    if (path === `/api/v1/integration-connections/${connection.ref}`) {
      await route.fulfill({ json: connection });
      return;
    }
    const base = `/api/v1/integration-connections/${connection.ref}/interaction-identities`;
    if (path === base && route.request().method() === "GET") {
      await route.fulfill({
        json: { items: identity ? [identity] : [], nextPageToken: "" },
      });
      return;
    }
    if (path === base && route.request().method() === "POST") {
      creates += 1;
      expect(route.request().headers()["if-match"]).toBe('"17"');
      expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      const input = route
        .request()
        .postDataJSON() as InteractionIdentityBindInput;
      expect(input).toEqual({
        externalTeamRef: "team_synthetic",
        externalChannelRef: "channel_synthetic",
        externalUserDigest: "b".repeat(64),
        subjectRef: "user_synthetic",
      });
      identity = {
        ...input,
        ref: "identity_synthetic",
        version: 1,
        connectionRef: connection.ref,
        connectionVersion: connection.version,
        state: "ACTIVE",
      };
      await route.fulfill({ status: 201, json: identity });
      return;
    }
    await route.fallback();
  });
  await page.route(
    "**/api/v1/interaction-identities/identity_synthetic",
    async (route) => {
      if (route.request().method() === "DELETE" && identity) {
        revokes += 1;
        expect(route.request().headers()["if-match"]).toBe('"1"');
        identity = { ...identity, version: 2, state: "REVOKED" };
        await route.fulfill({ json: identity });
        return;
      }
      await route.fallback();
    },
  );
  await page.route("**/api/v1/administration/access/subjects*", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            ref: "user_synthetic",
            kind: "USER",
            active: true,
            displayName: "Synthetic user",
          },
        ],
        nextPageToken: "",
      },
    }),
  );
  await page.goto("/integrations");
  await page
    .getByRole("button", { name: "Сведения о подключении", exact: true })
    .click();
  const details = page.getByRole("dialog", {
    name: connection.name,
    exact: true,
  });
  await details
    .getByRole("button", { name: "Привязать пользователя", exact: true })
    .click();
  const form = page.getByRole("dialog", {
    name: "Привязать пользователя",
    exact: true,
  });
  await form
    .getByLabel("ID команды Mattermost", { exact: true })
    .fill("team_synthetic");
  await form
    .getByLabel("ID канала Mattermost", { exact: true })
    .fill("channel_synthetic");
  await form
    .getByLabel("SHA256 ID пользователя Mattermost", { exact: true })
    .fill("b".repeat(64));
  await form
    .getByRole("button", { name: "Пользователь Kodex", exact: true })
    .click();
  await page.getByRole("option", { name: /Synthetic user/ }).click();
  await form
    .getByRole("button", { name: "Привязать пользователя", exact: true })
    .click();
  await expect(details.locator(".identity-row")).toHaveCount(1);
  expect(creates).toBe(1);
  await details
    .getByRole("button", { name: "Отозвать привязку", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Отозвать привязку", exact: true })
    .getByRole("button", { name: "Отозвать привязку", exact: true })
    .click();
  await expect(
    details.getByRole("button", { name: "Отозвать привязку", exact: true }),
  ).toHaveCount(0);
  expect(revokes).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await details
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
  await page
    .locator(".connections-panel")
    .getByRole("button", { name: "Загрузить ещё", exact: true })
    .click();
  await expect(page.locator(".connection-card")).toHaveCount(2);
  await page.locator(".connection-search input").fill("Нет совпадений");
  await expect.poll(() => queries.includes("Нет совпадений")).toBe(true);
  await expect(page.locator(".connection-card")).toHaveCount(0);
  await page.locator(".connection-search input").fill("");
  await expect(page.locator(".connection-card")).toHaveCount(1);
  await page
    .getByRole("button", { name: "Развернуть список", exact: true })
    .click();
  const expanded = page
    .getByRole("dialog")
    .filter({ has: page.locator(".connection-grid") });
  await expect(expanded.locator(".connection-card")).toHaveCount(1);
  await expanded
    .getByRole("button", { name: "Закрыть", exact: true })
    .last()
    .click();
}
