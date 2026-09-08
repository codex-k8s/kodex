import { expect, test } from "@playwright/test";

for (const project of [false, true])
  for (const allowed of [false, true]) {
    test(`synthetic: managed RoleImage source create project=${String(project)} allowed=${String(allowed)}`, async ({
      page,
      context,
    }) => {
      let queries = 0;
      const errors: string[] = [];
      page.on("pageerror", () => errors.push("PAGE_ERROR"));
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
      await page.route("https://kodex.test/**", async (route) => {
        const url = new URL(route.request().url());
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
          "/api/v1/administration/access/effective-access/query"
        ) {
          const request = route.request();
          expect(request.method()).toBe("POST");
          const target = project
            ? { kind: "PROJECT", projectRef: "project_fixture" }
            : { kind: "ORGANIZATION" };
          expect(request.postDataJSON()).toEqual({
            target,
            permissionKeys: ["image.source.view", "image.source.manage"],
          });
          queries++;
          await route.fulfill({
            json: {
              items: ["image.source.view", "image.source.manage"].map(
                (permissionKey) => ({
                  permissionKey,
                  target,
                  decision: allowed ? "ALLOWED" : "DENIED",
                  explanation: [],
                }),
              ),
            },
          });
          return;
        }
        if (url.pathname.startsWith("/api/")) {
          errors.push("UNEXPECTED_API");
          await route.abort();
          return;
        }
        const response = await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        });
        await route.fulfill({ response });
      });
      await page.goto(
        `/e2e/fixtures/impact.html?kind=source-create&project=${project ? "1" : "0"}`,
      );
      await expect.poll(() => queries).toBe(1);
      const name = page.locator(".configuration-editor__fields input");
      if (allowed) {
        await expect(name).toBeEnabled();
        await name.fill("Unsaved source fixture");
        await expect(name).toHaveValue("Unsaved source fixture");
      } else await expect(name).toBeDisabled();
      expect(errors).toEqual([]);
      await page.close();
    });
  }
