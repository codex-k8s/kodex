import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";

import { describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import HomeAttentionCenter from "./HomeAttentionCenter.vue";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

const source = readFileSync(
  new URL("./HomeAttentionCenter.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("HomeAttentionCenter states", () => {
  it("разделяет решения и аварийные запуски", () => {
    expect(template).toContain('v-if="gates.length"');
    expect(template).toContain('v-if="failedRuns.length"');
    expect(template).toContain("gate.contextSummary");
    expect(template).toContain("runListSummary(run)");
  });

  it("имеет отдельные loading, error и empty состояния", () => {
    expect(template).toContain('v-if="initialLoading"');
    expect(template).toContain('v-if="gatesProblem"');
    expect(template).toContain('v-if="runsProblem"');
    expect(template).toContain("total === 0");
  });

  it("показывает только подтверждённую потерю авторизации, без выдуманного срока", () => {
    expect(template).toContain('v-if="providerAccounts.length"');
    expect(template).toContain("account.state");
    expect(template).not.toContain("account.authorization?.expiresAt");
  });

  it("дозагружает предупреждения по cursor и оставляет явный retry ошибки", () => {
    expect(source).toContain("useAdaptiveCursorPageSize");
    expect(source).toContain("useCursorInfiniteScroll");
    expect(template).toContain('ref="providerSentinel"');
    expect(template).toContain("visibleLimit < total");
    expect(template).toContain("emit('retryMoreProviders', providerPageSize)");
  });

  it("ограничивает первый render пятью записями, сохраняя общий счётчик", async () => {
    const failedRuns = Array.from(
      { length: 15 },
      (_, index): Run => ({
        ref: `run_${String(index)}`,
        version: 1,
        rootRunRef: `run_${String(index)}`,
        sessionRef: `session_${String(index)}`,
        activitySummary: "Ошибка запуска",
        target: {
          type: "AGENT",
          ref: "agent_example",
          displayName: "Помощник",
          version: 1,
        },
        title: `Запуск ${String(index)}`,
        titleSource: "SERVER_DEFAULT",
        state: "FAILED",
        source: "CONTROL_CENTER",
        initiator: { ref: "owner", displayName: "Владелец" },
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
      }),
    );
    const app = createSSRApp({
      render: () =>
        h(HomeAttentionCenter, {
          gates: [],
          failedRuns,
          providerAccounts: [],
          projects: [],
          gatesReady: true,
          runsReady: true,
          providerReady: true,
        }),
    });
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
    const html = await renderToString(app);
    expect((html.match(/class="home-attention__item"/g) ?? []).length).toBe(5);
    expect(html).toMatch(/home-attention__count[^>]*>15</);
    expect(source).toContain("visibleLimit.value += 5");
    expect(source).toContain("max-height: min(420px, 55vh)");
    expect(source).toContain("overflow-y: auto");
    expect(html).toContain('tabindex="0"');
  });
});
