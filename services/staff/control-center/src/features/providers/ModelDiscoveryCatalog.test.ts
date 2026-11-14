import { createSSRApp, ref } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";

vi.mock("./model-discovery", () => ({
  useModelDiscovery: () => {
    const model = {
      providerId: "zai-coding-plan",
      providerName: "Z.AI Coding Plan",
      modelId: "glm-5.3",
      name: "GLM-5.3",
      family: "glm",
      contextTokens: "1000000",
      outputTokens: "131072",
      contextKnown: true,
      outputKnown: true,
      tools: "UNKNOWN",
      reasoning: "SUPPORTED",
      cache: "IMPLICIT",
      inputModalities: ["text"],
      outputModalities: ["text"],
      known: "KNOWN",
      adapter: "BUNDLED",
      account: "UNKNOWN",
      allowed: "UNAVAILABLE",
      tested: "NOT_RUN",
      routeConfigured: false,
    };
    return {
      query: ref("glm"),
      filters: {
        providerId: "",
        family: "",
        minimumContextTokens: 0,
        inputModality: "",
        outputModality: "",
        tools: "",
        reasoning: "",
        cache: "",
        groupBy: "PROVIDER",
      },
      groups: ref([{ id: "glm", label: "glm", items: [model] }]),
      items: ref([model]),
      source: ref({
        upstreamSchema: "models.dev/experimental-v2.0",
        sourceRevision: "a".repeat(40),
        catalogSha256: "b".repeat(64),
        retrievedAt: "2026-11-14T04:03:01Z",
      }),
      total: ref("1"),
      nextPageToken: ref(""),
      loading: ref(false),
      failed: ref(false),
      loaded: ref(true),
      search: vi.fn(),
    };
  },
}));
import ModelDiscoveryCatalog from "./ModelDiscoveryCatalog.vue";

describe("панель поиска моделей", () => {
  it("показывает владельцу все пять независимых статусов и отдельное отсутствие маршрута", async () => {
    const app = createSSRApp(ModelDiscoveryCatalog);
    app.use(createI18n({ legacy: false, locale: "ru", messages: {} }));
    const html = await renderToString(app);
    for (const text of [
      "Известна каталогу",
      "Адаптер в поставке",
      "Каталог аккаунта",
      "Право запуска",
      "Проверка исполнения",
      "Маршрут не настроен",
      "SDK поставляется; модель отдельно не проверена",
      "Исполнение не проверено",
      "GLM-5.3",
      "Z.AI Coding Plan",
    ])
      expect(html).toContain(text);
    expect(html).toContain("a".repeat(40));
    expect(html).toContain("b".repeat(64));
  });
});
