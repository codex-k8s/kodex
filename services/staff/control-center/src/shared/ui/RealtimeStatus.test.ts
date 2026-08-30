import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import RealtimeStatus from "@/shared/ui/RealtimeStatus.vue";
import {
  realtimeStatusPresentation,
  type RealtimeStatusState,
} from "@/shared/ui/realtime-status";

const labels: Record<RealtimeStatusState, string> = {
  "initial-loading": "Загружаем данные",
  live: "Данные поступают в реальном времени",
  "background-refresh": "Обновляем подтверждённые данные",
  reconnecting: "Восстанавливаем соединение",
  offline: "Показан последний подтверждённый снимок",
};

describe("realtimeStatusPresentation", () => {
  it("сохраняет текущие данные при refresh, reconnect и offline", () => {
    expect(
      realtimeStatusPresentation("initial-loading").preservesCurrentData,
    ).toBe(false);
    for (const state of [
      "background-refresh",
      "reconnecting",
      "offline",
    ] as const) {
      expect(realtimeStatusPresentation(state).preservesCurrentData).toBe(true);
    }
  });
});

describe("RealtimeStatus", () => {
  it.each([
    "initial-loading",
    "live",
    "background-refresh",
    "reconnecting",
    "offline",
  ] as const)("рендерит различимое состояние %s", async (state) => {
    const app = createSSRApp({
      render: () => h(RealtimeStatus, { labels, state }),
    });

    const html = await renderToString(app);

    expect(html).toContain('role="status"');
    expect(html).toContain(`data-state="${state}"`);
    expect(html).toContain(labels[state]);
    expect(html).toContain(
      `data-preserves-current-data="${state === "initial-loading" ? "false" : "true"}"`,
    );
  });

  it("показывает безопасную диагностику в tooltip без второй индикации", async () => {
    const app = createSSRApp({
      render: () =>
        h(RealtimeStatus, {
          detail: "Соединение будет восстановлено автоматически",
          labels,
          state: "reconnecting",
        }),
    });

    const html = await renderToString(app);

    expect(html).toContain(
      'title="Соединение будет восстановлено автоматически"',
    );
    expect(html.match(/role="status"/g)).toHaveLength(1);
  });
});
