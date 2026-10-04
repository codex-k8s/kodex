import { createPinia } from "pinia";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";

import { i18n } from "@/app/i18n";
import { usePlatformStore } from "@/features/platform/store";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import HomeResultCatalog from "./HomeResultCatalog.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

function run(index: number, state: Run["state"] = "RUNNING"): Run {
  return {
    ref: `run_fixture_${String(index)}`,
    version: 1,
    rootRunRef: `run_fixture_${String(index)}`,
    sessionRef: `ses_fixture_${String(index)}`,
    title: `Запуск ${String(index)}`,
    titleSource: "SERVER_DEFAULT",
    state,
    source: "CONTROL_CENTER",
    initiator: { ref: "owner_fixture", displayName: "Владелец" },
    target: {
      type: "AGENT",
      ref: "agt_fixture",
      displayName: "Сотрудник",
      version: 1,
    },
    activitySummary: "Текущая работа",
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 1,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
    nextActions: [],
    createdAt: "2026-10-04T11:00:00Z",
  };
}

async function render(runs: Run[]) {
  const pinia = createPinia();
  const platform = usePlatformStore(pinia);
  for (const item of runs) platform.runs[item.ref] = item;
  const app = createSSRApp({
    render: () => h(HomeResultCatalog, { kind: "RUN", dashboard: true }),
  });
  app.use(pinia);
  app.use(i18n);
  app.component(
    "RouterLink",
    defineComponent({
      setup:
        (_, { slots }) =>
        () =>
          h("a", slots.default?.()),
    }),
  );
  return renderToString(app);
}

describe("Каталог текущих запусков Главной", () => {
  it("сохраняет полный счётчик и передаёт все доступные записи в bounded список", async () => {
    const html = await render(
      Array.from({ length: 12 }, (_, index) => run(index)),
    );
    expect(html).toMatch(/<span[^>]*>12<\/span>/);
    expect((html.match(/class="home-result-row"/g) ?? []).length).toBe(5);
    expect(html).toContain("home-result-rows__sentinel");
    expect(html).toContain('tabindex="0"');
  });

  it("не включает terminal запуски в текущую работу и не добавляет пустой footer", async () => {
    const html = await render([run(0), run(1, "SUCCEEDED"), run(2, "FAILED")]);
    expect(html).toMatch(/<span[^>]*>1<\/span>/);
    expect((html.match(/class="home-result-row"/g) ?? []).length).toBe(1);
    expect(html).not.toContain("home-result-rows__sentinel");
    expect(html).not.toContain("<footer");
  });

  it("оставляет авторитетно пустой каталог пустым", async () => {
    const html = await render([]);
    expect(html).toMatch(/<span[^>]*>0<\/span>/);
    expect(html).not.toContain('class="home-result-row"');
    expect(html).not.toContain("home-result-rows__sentinel");
  });
});

