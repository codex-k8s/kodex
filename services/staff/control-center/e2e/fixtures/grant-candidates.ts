import { expect, type Page } from "@playwright/test";
import type {
  IntegrationConnection,
  IntegrationGrantCandidateContext,
  IntegrationGrantCandidatePins,
  IntegrationGrantConnectionCandidatePage,
  IntegrationGrantProjectCandidatePage,
  IntegrationGrantRecipientCandidatePage,
  IntegrationGrantCapabilityCandidatePage,
} from "../../src/shared/api/generated/openapi/types.gen";

export async function checkGrantCandidates(
  page: Page,
  projectRef: string,
): Promise<void> {
  let connection: IntegrationConnection = {
    ref: "connection_candidates",
    version: 7,
    definitionKey: "github",
    definitionVersion: "2.3",
    definitionDigest: "b".repeat(64),
    name: "Каталог разрешений",
    state: "CONNECTED",
    credentialsConfigured: true,
    credentialsHint: "Настроены",
    publicConfiguration: {},
    nextActions: ["MANAGE_GRANTS"],
    grants: [],
    capabilities: [
      {
        key: "github.read",
        name: "Читать репозиторий",
        description: "Точный ресурс",
        risk: "READ",
        approvalRequired: false,
        approvalPolicy: "NONE",
        operation: "read",
        resourceKind: "GITHUB_REPOSITORY",
        inputFields: [],
      },
    ],
  };
  const pins: IntegrationGrantCandidatePins = {
    contextDigest: "a".repeat(64),
    connectionVersion: 7,
    definitionVersion: "2.3",
    definitionDigest: "b".repeat(64),
  };
  const reads: string[] = [];
  let grants = 0;
  await page.route("**/api/v1/integration-connections*", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path !== "/api/v1/integration-connections") return route.fallback();
    await route.fulfill({ json: { items: [connection], total: 1 } });
  });
  await page.route(
    `**/api/v1/integration-connections/${connection.ref}`,
    (route) => route.fulfill({ json: connection }),
  );
  await page.route(
    "**/api/v1/integration-grant-candidates/*",
    async (route) => {
      const url = new URL(route.request().url());
      const kind = url.pathname.split("/").at(-1);
      reads.push(kind ?? "");
      const context: IntegrationGrantCandidateContext = {};
      if (kind !== "connections") {
        expect(url.searchParams.get("connectionRef")).toBe(connection.ref);
        context.connectionRef = connection.ref;
      }
      if (kind === "recipients" || kind === "capabilities") {
        expect(url.searchParams.get("projectRef")).toBe(projectRef);
        expect(url.searchParams.get("recipientKind")).toBe("AGENT");
        context.projectRef = projectRef;
        context.recipientKind = "AGENT";
      }
      if (kind === "capabilities") {
        expect(url.searchParams.get("recipientRef")).toBe("agent_candidate");
        context.recipientRef = "agent_candidate";
      }
      const base = { context, contextDigest: pins.contextDigest, pins };
      if (kind === "connections") {
        expect(url.searchParams.get("purpose")).toBe("GRANT");
        const result: IntegrationGrantConnectionCandidatePage = {
          ...base,
          total: 2,
          items: [
            {
              connectionRef: "connection_denied",
              name: "Недоступное подключение",
              definitionKey: "github",
              providerName: "GitHub",
              resourceScope: {},
              grantable: false,
              usable: false,
              reason: "CONNECTION_UNAVAILABLE",
              pins,
            },
            {
              connectionRef: connection.ref,
              name: connection.name,
              definitionKey: "github",
              providerName: "GitHub",
              credentialKind: "TOKEN",
              resourceScope: { repository: "synthetic/repo" },
              grantable: true,
              usable: false,
              reason: "READY",
              pins,
            },
          ],
        };
        await route.fulfill({ json: result });
        return;
      }
      if (kind === "projects") {
        const projectPins = { ...pins, projectVersion: 3 };
        const result: IntegrationGrantProjectCandidatePage = {
          ...base,
          total: 1,
          items: [
            {
              projectRef,
              name: "Проект для разрешения",
              grantable: true,
              reason: "READY",
              pins: projectPins,
            },
          ],
        };
        await route.fulfill({ json: result });
        return;
      }
      if (kind === "recipients") {
        const targetPins = { ...pins, projectVersion: 3, recipientVersion: 8 };
        const result: IntegrationGrantRecipientCandidatePage = {
          ...base,
          pins: { ...pins, projectVersion: 3 },
          total: 2,
          items: [
            {
              recipientRef: "agent_denied",
              name: "Недоступный получатель",
              recipientKind: "AGENT",
              projectRef,
              grantable: false,
              reason: "RECIPIENT_UNAVAILABLE",
              pins: targetPins,
            },
            {
              recipientRef: "agent_candidate",
              name: "Получатель разрешения",
              recipientKind: "AGENT",
              projectRef,
              grantable: true,
              reason: "READY",
              pins: targetPins,
            },
          ],
        };
        await route.fulfill({ json: result });
        return;
      }
      if (kind === "capabilities") {
        const capability = connection.capabilities[0];
        if (!capability) throw new Error("Missing capability fixture");
        const finalPins = { ...pins, projectVersion: 3, recipientVersion: 8 };
        const result: IntegrationGrantCapabilityCandidatePage = {
          ...base,
          pins: finalPins,
          total: 1,
          items: [
            {
              capability,
              grantable: true,
              reason: "READY",
              pins: finalPins,
            },
          ],
        };
        await route.fulfill({ json: result });
        return;
      }
      throw new Error("Unexpected candidate kind");
    },
  );
  await page.route(
    `**/api/v1/integration-connections/${connection.ref}/grants`,
    async (route) => {
      expect(route.request().headers()["if-match"]).toBe('"7"');
      expect(route.request().headers()["idempotency-key"]).toBeTruthy();
      expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
      expect(route.request().postDataJSON()).toEqual({
        capabilityKey: "github.read",
        agentRef: "agent_candidate",
        enabled: true,
      });
      grants++;
      connection = {
        ...connection,
        version: 8,
        grants: [
          {
            ref: "grant_candidate",
            version: 1,
            targetName: "Получатель разрешения",
            risk: "READ",
            approvalPolicy: "NONE",
            resourceScope: {
              kind: "GITHUB_REPOSITORY",
              values: { repository: "synthetic/repo" },
              digest: "c".repeat(64),
            },
            capabilityKey: "github.read",
            agentRef: "agent_candidate",
            enabled: true,
          },
        ],
      };
      await route.fulfill({ json: connection });
    },
  );
  await page.goto("/integrations");
  await page.getByRole("tab", { name: /^Разрешения\s*0$/ }).click();
  const panel = page.locator(".grant-panel");
  await panel.getByRole("button", { name: "Подключение", exact: true }).click();
  await expect(
    page.getByRole("option", { name: /Недоступное подключение/ }),
  ).toBeDisabled();
  await page.getByRole("option", { name: /Каталог разрешений/ }).click();
  await panel.getByRole("button", { name: "Проект", exact: true }).click();
  await page.getByRole("option", { name: /Проект для разрешения/ }).click();
  await panel
    .getByRole("button", { name: "Получатель разрешения", exact: true })
    .click();
  await expect(
    page.getByRole("option", { name: /Недоступный получатель/ }),
  ).toBeDisabled();
  await page.getByRole("option", { name: /Получатель разрешения/ }).click();
  await panel.getByRole("button", { name: "Возможность", exact: true }).click();
  await page.getByRole("option", { name: /Читать репозиторий/ }).click();
  await panel
    .getByRole("button", { name: "Выдать разрешение", exact: true })
    .click();
  await expect.poll(() => grants).toBe(1);
  await expect(
    panel.getByRole("button", { name: "Выдать разрешение", exact: true }),
  ).toBeDisabled();
  expect(new Set(reads)).toEqual(
    new Set(["connections", "projects", "recipients", "capabilities"]),
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
