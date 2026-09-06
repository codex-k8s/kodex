import { expect, test } from "@playwright/test";
import type {
  RuntimeSecret,
  RuntimeSecretDraft,
  RuntimeSecretDraftImpactPlan,
} from "../src/shared/api/generated/openapi/types.gen";

for (const width of [390, 2900]) {
  test(`synthetic: staged rotation и потеря ACK ${String(width)}px`, async ({
    page,
    context,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const failures: string[] = [];
    page.on("pageerror", (error) => failures.push(error.message));
    let saves = 0;
    let publications = 0;
    let saveKey = "";
    let saveInput: unknown;
    const secret: RuntimeSecret = {
      ref: "secret_rotation",
      projectRef: "project_rotation",
      version: 8,
      name: "ROTATION_SYNTHETIC",
      description: "",
      valueType: "JSON",
      currentRevision: 4,
      state: "ACTIVE",
      nextActions: ["ROTATE"],
      createdAt: "2026-09-05T00:00:00Z",
      updatedAt: "2026-09-05T00:00:00Z",
    };
    let draft: RuntimeSecretDraft = {
      ref: "draft_rotation",
      version: 1,
      generation: 4,
      projectRef: "project_rotation",
      secretRef: secret.ref,
      secretVersion: 7,
      name: secret.name,
      description: "",
      valueType: "JSON",
      state: "DRAFT",
      publishedRevision: 0,
      createdAt: secret.createdAt,
      updatedAt: secret.updatedAt,
      expiresAt: "2026-09-06T00:00:00Z",
    };
    let plan: RuntimeSecretDraftImpactPlan = {
      ref: "plan_rotation",
      draftRef: draft.ref,
      draftVersion: 2,
      secretRef: secret.ref,
      secretVersion: 7,
      sourceRevision: 3,
      digest: "a".repeat(64),
      total: 3,
      expiresAt: draft.expiresAt,
      state: "PREPARED",
    };
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
    await page.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
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
      if (request.method() === "POST") {
        expect(request.headers()["x-csrf-token"]).toBeTruthy();
        expect(request.headers()["idempotency-key"]).toBeTruthy();
      }
      if (url.pathname === "/api/v1/runtime-secrets/secret_rotation/drafts") {
        saves++;
        expect(request.headers()["if-match"]).toBe('"7"');
        if (saves === 1) {
          saveKey = request.headers()["idempotency-key"] ?? "";
          saveInput = request.postDataJSON();
          expect(saveInput).toEqual({
            valueType: "JSON",
            value: '{"rotation":"synthetic"}',
          });
          await route.fulfill({
            status: 503,
            json: { status: 503, code: "SERVICE_UNAVAILABLE" },
          });
          return;
        }
        expect(request.headers()["idempotency-key"]).toBe(saveKey);
        expect(request.postDataJSON()).toEqual(saveInput);
        await route.fulfill({ json: draft });
        return;
      }
      if (url.pathname === "/api/v1/runtime-secret-drafts/draft_rotation") {
        await route.fulfill({ json: draft });
        return;
      }
      if (
        url.pathname === "/api/v1/runtime-secret-drafts/draft_rotation/validate"
      ) {
        expect(request.headers()["if-match"]).toBe('"1"');
        draft = { ...draft, version: 2, state: "VALID" };
        await route.fulfill({ json: draft });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/runtime-secret-drafts/draft_rotation/impact-plans"
      ) {
        expect(request.headers()["if-match"]).toBe('"2"');
        await route.fulfill({ json: plan });
        return;
      }
      if (
        url.pathname ===
        "/api/v1/runtime-secret-draft-impact-plans/plan_rotation"
      ) {
        expect(url.searchParams.has("pageToken")).toBe(false);
        await route.fulfill({
          json: {
            plan,
            total: 3,
            nextPageToken: "",
            items: ["APPLIED", "CONFLICT", "FORBIDDEN"].map(
              (outcome, index) => ({
                ref: `item_rotation_${String(index)}`,
                consumer: {
                  environmentRef: `environment_rotation_${String(index)}`,
                  environmentVersion: 9,
                  environmentVersionRef: `version_rotation_${String(index)}`,
                  projectRef: "project_rotation",
                  secretRevisions: [3],
                },
                outcome: plan.state === "APPLIED" ? outcome : "PENDING",
                ...(plan.state === "APPLIED" && index === 0
                  ? {
                      resultEnvironmentVersionRef:
                        "published_environment_rotation",
                    }
                  : {}),
              }),
            ),
          },
        });
        return;
      }
      if (
        url.pathname === "/api/v1/runtime-secret-drafts/draft_rotation/publish"
      ) {
        publications++;
        expect(request.headers()["if-match"]).toBe('"2"');
        expect(request.postDataJSON()).toEqual({
          expectedSecretVersion: 7,
          impactPlanRef: "plan_rotation",
          selectedItemRefs: [
            "item_rotation_0",
            "item_rotation_1",
            "item_rotation_2",
          ],
        });
        draft = {
          ...draft,
          version: 3,
          secretVersion: 8,
          state: "PUBLISHED",
          publishedRevision: 4,
        };
        plan = { ...plan, state: "APPLIED" };
        await route.fulfill({
          status: 503,
          json: { status: 503, code: "SERVICE_UNAVAILABLE" },
        });
        return;
      }
      if (url.pathname === "/api/v1/runtime-secrets/secret_rotation") {
        await route.fulfill({ json: secret });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push(`Unhandled API ${url.pathname}`);
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/impact.html?kind=rotation");
    let dialog = page.getByRole("dialog");
    await dialog
      .getByLabel("Секретное значение", { exact: true })
      .fill('{"rotation":"synthetic"}');
    await dialog
      .getByRole("button", { name: "Сохранить черновик", exact: true })
      .click();
    await expect(
      dialog.getByLabel("Секретное значение", { exact: true }),
    ).toBeDisabled();
    await dialog
      .getByRole("button", { name: "Повторить исходный запрос", exact: true })
      .click();
    await expect(dialog).toContainText("draft_rotation");
    expect(saves).toBe(2);
    await expect(
      dialog.getByLabel("Секретное значение", { exact: true }),
    ).toHaveCount(0);
    await dialog
      .getByRole("button", { name: "Проверить черновик", exact: true })
      .click();
    await dialog
      .getByRole("button", { name: "Подготовить план влияния", exact: true })
      .click();
    await expect(dialog.getByRole("checkbox")).toHaveCount(3);
    await dialog
      .getByRole("button", {
        name: "Опубликовать и применить выбранное",
        exact: true,
      })
      .click();
    await expect(
      dialog.getByRole("button", {
        name: "Повторить исходный запрос",
        exact: true,
      }),
    ).toBeVisible();
    await expect(page).toHaveURL(/planRef=plan_rotation/);
    page.on("dialog", (value) => value.accept());
    await page.reload();
    dialog = page.getByRole("dialog");
    await expect(dialog).toContainText("PUBLISHED");
    await expect(dialog.locator("dl")).toContainText(
      /Версия черновика\s*3\s*Опубликованная ревизия\s*4/,
    );
    await expect(dialog).not.toContainText("Выбрано: 0");
    await expect(dialog).toContainText("published_environment_rotation");
    await expect(dialog).toContainText("CONFLICT");
    await expect(dialog).toContainText("FORBIDDEN");
    await expect(page.getByTestId("rotation-published")).toHaveText(
      "secret_rotation · 4",
    );
    expect(publications).toBe(1);
    expect(saves).toBe(2);
    const persisted = await page.evaluate(() => ({
      values: [
        ...Object.keys(localStorage).map((key) => localStorage.getItem(key)),
        ...Object.keys(sessionStorage).map((key) =>
          sessionStorage.getItem(key),
        ),
      ],
      queryKeys: [...new URL(location.href).searchParams.keys()],
    }));
    for (const value of persisted.values)
      expect(value).not.toContain('"rotation":"synthetic"');
    expect(persisted.queryKeys.sort()).toEqual(["draftRef", "kind", "planRef"]);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
    ).toBe(false);
    await page.screenshot({
      path: testInfo.outputPath(`rotation-${String(width)}.png`),
      fullPage: true,
    });
    expect(failures).toEqual([]);
  });
}
