import { expect, test, type Route } from "@playwright/test";
import type {
  ModelDiscoveryModel,
  ModelDiscoveryPage,
} from "../src/shared/api/generated/openapi/types.gen";
import { handleSyntheticRuntimeRequest } from "./synthetic-runtime-route";

// Полные synthetic wire metadata не являются observation живого account.
function model(modelId: string): ModelDiscoveryModel {
  return {
    providerId: "zai-coding-plan",
    providerName: "Z.ai Coding Plan",
    modelId,
    name: modelId,
    type: "chat",
    family: "glm",
    contextTokens: "1000000",
    outputTokens: "32768",
    contextKnown: true,
    outputKnown: true,
    tools: "SUPPORTED",
    reasoning: "SUPPORTED",
    reasoningEfforts: [],
    inputModalities: ["text", "image/png"],
    outputModalities: ["text"],
    cache: "UNKNOWN",
    cacheImplicit: "UNKNOWN",
    cacheExplicit: "UNKNOWN",
    upstreamStatus: "UNKNOWN",
    known: "KNOWN",
    adapter: "BUNDLED",
    account: "UNKNOWN",
    allowed: "UNAVAILABLE",
    tested: "NOT_RUN",
    routeConfigured: false,
  };
}
function catalog(
  items: ModelDiscoveryModel[],
  nextPageToken = "",
  groupBy: "PROVIDER" | "FAMILY" = "PROVIDER",
): ModelDiscoveryPage {
  return {
    items,
    nextPageToken,
    groupBy,
    total: "2",
    source: {
      schema: "kodex.model-provider-discovery/v1",
      source: "models.dev",
      license: "MIT",
      sourceRevision: "a".repeat(40),
      upstreamSchema: "models.dev/experimental-v2.0",
      schemaSha256: "b".repeat(64),
      upstreamSha256: "c".repeat(64),
      snapshotSha256: "d".repeat(64),
      sourceUrl: "https://models.dev/_experimental_v2.0_api.json",
      modelCount: 2,
      overlaySha256: "e".repeat(64),
      catalogSha256: "f".repeat(64),
      retrievedAt: "2026-11-14T04:03:01Z",
      projectionSchema: "urn:kodex:contract:model-provider-discovery:v1",
      projectionSchemaSha256: "1".repeat(64),
      upstreamSchemaValidation: "NOT_PERFORMED",
    },
  };
}

for (const width of [1440, 390]) {
  test(`synthetic: поиск моделей, фильтры, cursor и смена owner ${String(width)}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const failures: string[] = [];
    const requests: URL[] = [];
    let pending: Route | undefined;
    page.on("pageerror", (error) => failures.push(error.message));
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (await handleSyntheticRuntimeRequest(route, url, failures)) return;
      if (url.pathname === "/api/v1/model-discovery") {
        expect(route.request().method()).toBe("GET");
        expect(route.request().headers()).not.toHaveProperty(
          "x-kodex-project-id",
        );
        requests.push(url);
        const query = url.searchParams.get("query");
        if (query === "old-owner") {
          pending = route;
          return;
        }
        if (query === "empty") {
          await route.fulfill({ json: { ...catalog([]), total: "0" } });
          return;
        }
        if (query === "error") {
          await route.fulfill({
            status: 503,
            json: { title: "Unavailable", status: 503 },
          });
          return;
        }
        const filtered = url.searchParams.get("groupBy") === "FAMILY";
        const cursor = url.searchParams.get("pageToken");
        await route.fulfill({
          json: catalog(
            [
              model(
                query === "new-owner"
                  ? "new-owner-model"
                  : cursor
                    ? "glm-5.3-flash"
                    : "glm-5.3",
              ),
            ],
            query === "new-owner" || cursor ? "" : "catalog-next",
            filtered ? "FAMILY" : "PROVIDER",
          ),
        });
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        failures.push("unexpected API");
        await route.abort();
        return;
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
    await page.goto("/e2e/fixtures/model-discovery.html");
    await page
      .getByText("Каталог провайдеров и моделей", { exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "glm-5.3", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Исполнение не проверено", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("Есть доступный аккаунт этого маршрута", { exact: true }),
    ).toHaveCount(0);
    await page.getByLabel("Найти модель или провайдера").fill("glm");
    await page.getByLabel("Код провайдера").fill("zai-coding-plan");
    await page.getByLabel("Семейство модели").fill("glm");
    await page.getByLabel("Минимальный контекст, токены").fill("1000000");
    await page.getByLabel("Входная модальность").fill("image");
    await page.getByLabel("Выходная модальность").fill("text");
    await page.getByLabel("Вызовы инструментов").selectOption("SUPPORTED");
    await page.getByLabel("Рассуждения").selectOption("SUPPORTED");
    await page.getByLabel("Кеш upstream").selectOption("UNKNOWN");
    await page.getByLabel("Группировать").selectOption("FAMILY");
    await page.getByRole("button", { name: "Найти", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "glm", exact: true }),
    ).toBeVisible();
    const filtered = requests.at(-1);
    expect(filtered && Object.fromEntries(filtered.searchParams)).toEqual({
      pageSize: "20",
      query: "glm",
      providerId: "zai-coding-plan",
      family: "glm",
      minimumContextTokens: "1000000",
      inputModality: "image",
      outputModality: "text",
      tools: "SUPPORTED",
      reasoning: "SUPPORTED",
      cache: "UNKNOWN",
      groupBy: "FAMILY",
    });
    // Изменённая форма не подменяет applied filters следующей страницы.
    await page.getByLabel("Код провайдера").fill("unsaved-provider");
    await page.getByRole("button", { name: "Показать ещё" }).click();
    await expect(
      page.getByRole("heading", { name: "glm-5.3-flash", exact: true }),
    ).toBeVisible();
    expect(requests.at(-1)?.searchParams.get("providerId")).toBe(
      "zai-coding-plan",
    );
    expect(requests.at(-1)?.searchParams.get("pageToken")).toBe("catalog-next");
    await page.getByText("Источник и ревизия", { exact: true }).click();
    await expect(page.locator("time")).toHaveAttribute(
      "datetime",
      "2026-11-14T04:03:01Z",
    );
    await page.getByLabel("Найти модель или провайдера").fill("old-owner");
    await page.getByRole("button", { name: "Найти", exact: true }).click();
    await expect.poll(() => Boolean(pending)).toBe(true);
    await page.getByRole("button", { name: "Сменить synthetic owner" }).click();
    await expect(
      page.getByRole("heading", { name: "glm-5.3-flash", exact: true }),
    ).toHaveCount(0);
    await expect(page.locator("time")).toHaveCount(0);
    await expect(page.getByLabel("Найти модель или провайдера")).toHaveValue(
      "",
    );
    await expect(page.getByLabel("Код провайдера")).toHaveValue("");
    await expect(page.getByLabel("Группировать")).toHaveValue("PROVIDER");
    await expect(
      page.getByRole("button", { name: "Найти", exact: true }),
    ).toBeEnabled();
    await page.getByLabel("Найти модель или провайдера").fill("new-owner");
    await page.getByRole("button", { name: "Найти", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "new-owner-model", exact: true }),
    ).toBeVisible();
    if (!pending) throw new Error("Synthetic pending route is absent");
    await pending.fulfill({ json: catalog([model("late-old-owner-model")]) });
    await expect(
      page.getByRole("heading", { name: "late-old-owner-model", exact: true }),
    ).toHaveCount(0);
    for (const [query, expected] of [
      ["empty", "По этому запросу моделей нет."],
      ["error", "Каталог недоступен. Повторите поиск."],
    ] as const) {
      await page.getByLabel("Найти модель или провайдера").fill(query);
      await page.getByRole("button", { name: "Найти", exact: true }).click();
      await expect(page.getByText(expected, { exact: true })).toBeVisible();
    }
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(failures).toEqual([]);
  });
}
