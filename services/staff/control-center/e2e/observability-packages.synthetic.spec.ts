import { expect, test } from "@playwright/test";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

const profiles = [
  "sentry",
  "sentry-self-hosted",
  "sentry-organization",
  "sentry-self-hosted-organization",
  "prometheus",
  "thanos",
  "grafana-dashboard",
  "grafana-organization",
  "grafana-dashboard-annotations",
  "grafana-organization-rule-delete",
  "loki",
  "loki-ruler-namespace-read",
  "loki-ruler-namespace",
];

test("реальные shipped профили показывают явную область и закрытые публичные поля", async ({
  page,
}) => {
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
    if (url.pathname.startsWith("/api/")) {
      failures.push("Unexpected synthetic provider/API request");
      await route.abort();
      return;
    }
    await route.fulfill({
      response: await route.fetch({
        url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
      }),
    });
  });
  await page.goto("/e2e/fixtures/observability-packages.html");
  const chosen = page.getByRole("region", { name: "Выбранный профиль" });
  await expect(
    page.getByRole("heading", { name: "Профили наблюдаемости" }),
  ).toBeVisible();
  for (const key of profiles) {
    await page.getByLabel("Профиль подключения").selectOption(key);
    await expect(chosen.getByRole("heading")).toBeVisible();
    await expect(chosen.locator('[name$="base_url"]')).toHaveCount(1);
    if (key !== "sentry" && key !== "sentry-organization")
      await expect(chosen.locator('input[type="url"]')).toHaveCount(1);
    await expect(chosen.locator('input[type="password"]')).toHaveCount(0);
    await expect(chosen.locator('input[name$="api_token"]')).toHaveCount(0);
    await expect(chosen.locator('input[name*="qualification"]')).toHaveCount(0);
    const hasProject = key === "sentry" || key === "sentry-self-hosted";
    await expect(chosen.locator('[name$="project_slug"]')).toHaveCount(
      hasProject ? 1 : 0,
    );
    const hasDatasource =
      key === "grafana-dashboard" || key === "grafana-organization";
    await expect(chosen.locator('[name$="datasource_uid"]')).toHaveCount(
      hasDatasource ? 1 : 0,
    );
    const qualified = [
      "grafana-dashboard-annotations",
      "grafana-organization-rule-delete",
      "loki-ruler-namespace",
    ].includes(key);
    await expect(chosen.locator('p[role="status"]')).toHaveCount(
      qualified ? 1 : 0,
    );
    if (qualified)
      await expect(chosen.locator('p[role="status"]')).not.toContainText(
        "i18n:",
      );
    if (key === "grafana-dashboard-annotations") {
      await expect(chosen.locator('[name$="scope_mode"] option')).toHaveText([
        "",
        "DASHBOARD_ANNOTATIONS",
      ]);
      await chosen
        .getByLabel("UID выбранного dashboard", { exact: false })
        .fill("fixture-dashboard");
    }
    if (key === "grafana-organization-rule-delete")
      await expect(chosen.locator('[name$="scope_mode"] option')).toHaveText([
        "",
        "API_ONLY_ORGANIZATION_RULE_DELETE",
      ]);
    if (key.startsWith("loki-ruler"))
      await expect(chosen.locator('[name$="rule_namespace"]')).toHaveCount(1);
  }
  expect(failures).toEqual([]);
});
