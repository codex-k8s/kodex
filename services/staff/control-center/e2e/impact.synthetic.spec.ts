import { expect, test } from "@playwright/test";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
  RuntimeEnvironmentConsumer,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [1440, 390]) {
  for (const kind of ["environment", "secret", "managed"] as const) {
    test(`synthetic: impact search и rebind ${kind} ${String(width)}px`, async ({
      page,
      context,
    }, testInfo) => {
      await page.setViewportSize({ width, height: 844 });
      const failures: string[] = [];
      const queries: { query: string; cursor: string | null }[] = [];
      let mutations = 0;
      page.on("pageerror", (error) => failures.push(error.message));
      page.on("console", (message) => {
        if (["warning", "error"].includes(message.type()))
          failures.push(message.text());
      });
      page.on("requestfailed", (request) => {
        if (request.failure()?.errorText !== "net::ERR_ABORTED")
          failures.push("Failed request");
      });
      await context.addCookies([
        {
          name: "__Host-kodex-csrf",
          value: "s".repeat(43),
          domain: "kodex.test",
          path: "/",
          secure: true,
          sameSite: "Strict",
        },
      ]);
      const digest = "a".repeat(64);
      const revision: ManagedConfigurationRevision = {
        ref: "target",
        revision: 2,
        state: "PUBLISHED",
        contentFormat: "TEXT",
        content: "Synthetic",
        digest,
        validationDiagnostics: [],
        createdAt: "2026-09-05T00:00:00Z",
      };
      const configuration: ManagedConfiguration = {
        ref: "configuration",
        version: 1,
        kind: "PROMPT_TEMPLATE",
        name: "Конфигурация",
        managedBy: "UI",
        source: "UI",
        sourceRevision: "",
        currentRevision: revision,
        updatedAt: revision.createdAt,
      };
      const consumer = (id: string): RuntimeEnvironmentConsumer => ({
        agentRef: id,
        agentVersion: 3,
        bindingRef: `binding-${id}`,
        bindingVersion: 4,
        versionRef: "old",
        projectRef: "project",
      });
      const row = (id: string) =>
        kind === "environment"
          ? consumer(id)
          : kind === "secret"
            ? {
                environmentRef: id,
                environmentVersion: 19,
                environmentVersionRef: "old",
                projectRef: "project",
                secretRevisions: [6],
              }
            : { kind: "AGENT", ref: id, revisionRef: "old", version: 4 };
      const path =
        kind === "environment"
          ? "/api/v1/runtime-environments/environment/versions/target"
          : kind === "secret"
            ? "/api/v1/runtime-secrets/secret/revisions/7"
            : "/api/v1/managed-configurations/configuration/revisions/target";
      const rebindPath =
        kind === "managed"
          ? "/api/v1/prompt-template-configurations/configuration/revisions/target/consumer-bindings"
          : `${path}/consumer-bindings`;
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
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
        if (
          url.pathname ===
          "/api/v1/managed-configurations/configuration/revisions"
        ) {
          await route.fulfill({
            json: { configuration, items: [revision], total: 1 },
          });
          return;
        }
        if (url.pathname === `${path}/impact`) {
          const query = url.searchParams.get("query") ?? "";
          const cursor = url.searchParams.get("pageToken");
          queries.push({ query, cursor });
          expect(url.searchParams.get("pageSize")).toBe("40");
          if (query) expect(cursor).toBeNull();
          const consumers =
            query === "second" || cursor ? [row("second")] : [row("first")];
          const common = {
            consumers,
            total: query ? 1 : 2,
            nextPageToken: query || cursor ? "" : "next",
          };
          const data =
            kind === "environment"
              ? {
                  ...common,
                  environmentRef: "environment",
                  environmentVersion: 19,
                  targetVersionRef: "target",
                  targetDigest: digest,
                }
              : kind === "secret"
                ? {
                    ...common,
                    secretRef: "secret",
                    secretVersion: 23,
                    targetRevision: 7,
                  }
                : {
                    ...common,
                    configurationRef: "configuration",
                    targetRevisionRef: "target",
                    digest,
                  };
          await route.fulfill({ json: data });
          return;
        }
        if (url.pathname === rebindPath) {
          expect(route.request().method()).toBe("POST");
          expect(route.request().headers()["idempotency-key"]).toBeTruthy();
          expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
          expect(route.request().headers()["if-match"]).toBe(
            kind === "environment"
              ? '"19"'
              : kind === "secret"
                ? '"23"'
                : '"1"',
          );
          const body: unknown = route.request().postDataJSON();
          expect(body).toEqual(
            kind === "secret"
              ? {
                  selections: [
                    {
                      environmentRef: "second",
                      expectedEnvironmentVersion: 19,
                      sourceVersionRef: "old",
                      consumers: [],
                    },
                  ],
                }
              : kind === "managed"
                ? { impactDigest: digest, consumers: [row("second")] }
                : { consumers: [consumer("second")] },
          );
          mutations += 1;
          await route.fulfill({
            json:
              kind === "environment"
                ? {
                    bindings: [
                      {
                        ref: "binding-second",
                        version: 5,
                        agentRef: "second",
                        environmentRef: "environment",
                        versionRef: "target",
                        digest,
                      },
                    ],
                  }
                : kind === "secret"
                  ? {
                      environments: [
                        {
                          environmentRef: "second",
                          environmentVersion: 20,
                          versionRef: "new",
                          projectRef: "project",
                          digest,
                        },
                      ],
                      bindings: [],
                    }
                  : {
                      configuration: { ...configuration, version: 2 },
                      revision,
                    },
          });
          return;
        }
        if (url.pathname.startsWith("/api/")) {
          failures.push(`Unhandled API ${url.pathname}`);
          await route.abort();
          return;
        }
        const response = await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        });
        if (!response.ok()) failures.push("Failed asset");
        await route.fulfill({ response });
      });
      await page.goto(`/e2e/fixtures/impact.html?kind=${kind}`);
      if (kind === "managed")
        await page
          .getByRole("button", { name: "Влияние ревизии", exact: true })
          .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByRole("checkbox")).toHaveCount(1);
      await dialog.getByRole("button", { name: /ещё/i }).click();
      await expect(dialog.getByRole("checkbox")).toHaveCount(2);
      expect(queries).toEqual([
        { query: "", cursor: null },
        { query: "", cursor: "next" },
      ]);
      await dialog.getByRole("checkbox").first().check();
      await dialog.getByRole("searchbox").fill("second");
      await expect(dialog.getByRole("checkbox")).toHaveCount(1);
      await expect(dialog.getByRole("checkbox")).not.toBeChecked();
      expect(queries.at(-1)).toEqual({ query: "second", cursor: null });
      await expect(
        dialog.getByRole("button", { name: /Перепривязать/ }),
      ).toBeDisabled();
      await dialog.getByRole("checkbox").check();
      await dialog.screenshot({
        path: testInfo.outputPath(`impact-${kind}-${String(width)}.png`),
      });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      await dialog.getByRole("button", { name: /Перепривязать/ }).click();
      await expect.poll(() => mutations).toBe(1);
      if (kind === "managed") await expect(dialog).toHaveCount(0);
      else
        await expect(dialog.locator(".impact-receipt")).toContainText("second");
      expect(failures).toEqual([]);
    });
  }
}
