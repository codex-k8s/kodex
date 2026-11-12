import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import ModelMeasurements from "./ModelMeasurements.vue";
import { activeTranscriptItemId, type RunActivityItem } from "./run-activity";
import { validModelMeasurement } from "@/shared/api/model-measurement";

it("показывает отдельное сжатие, UNKNOWN TTFT и не переводит измерение в активную фазу", async () => {
  const execution = {
    runRef: "run_fixture",
    nodeRef: "nod_fixture",
    sessionRef: "ses_fixture",
    turnRef: "trn_fixture",
    turnNumber: 1,
    attempt: 1,
  };
  const working: RunActivityItem = {
    id: "evt_working",
    kind: "system",
    actor: "Kodex",
    occurredAt: "2026-11-12T00:00:00Z",
    historical: false,
    state: "RUNNING",
    execution,
    serviceProgressCode: "MODEL_CONTEXT_COMPACTING",
  };
  const measured: RunActivityItem = {
    ...working,
    id: "evt_measurement",
    modelMeasurement: {
      ref: "step_fixture01",
      source: "NATIVE_STEP",
      phase: "COMPACTION",
      outcome: "COMPLETED",
      completeness: "PARTIAL",
      usage: {
        completeness: "PARTIAL",
        inputTokens: 100,
        outputTokens: 10,
        totalTokens: 110,
        cachedInputTokens: 50,
        cacheWriteInputTokens: 20,
        reasoningOutputTokens: 0,
        modelContextWindow: 0,
      },
    },
  };
  const measurement = measured.modelMeasurement;
  if (!measurement) throw new Error("Measurement fixture is missing");
  expect(activeTranscriptItemId([working, measured])).toBe(working.id);
  expect(validModelMeasurement(measurement)).toBe(true);
  expect(
    validModelMeasurement({
      ...measurement,
      completeness: "COMPLETE",
    }),
  ).toBe(false);
  const app = createSSRApp({
    render: () =>
      h(ModelMeasurements, {
        items: [working, measured, { ...measured, id: "evt_duplicate" }],
      }),
  });
  app.use(i18n);
  const previous = i18n.global.locale.value;
  i18n.global.locale.value = "ru";
  try {
    const html = await renderToString(app);
    expect(html).toContain("Сжатие контекста");
    expect(html).toContain("Стоимость неизвестна");
    expect(html).toContain("Неизвестно");
    expect(html).toContain(">100</td>");
    expect(html).not.toContain(">200</td>");
    expect(html).not.toContain("step_fixture01");
  } finally {
    i18n.global.locale.value = previous;
  }
});

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

it("отклоняет неизвестный wire source и неверную внешнюю форму измерения", () => {
  for (const value of [
    null,
    undefined,
    0,
    [],
    {
      ref: "request_fixture01",
      source: "UNREGISTERED",
      phase: "NORMAL",
      outcome: "FAILED",
      completeness: "UNKNOWN",
    },
  ])
    expect(validModelMeasurement(value)).toBe(false);
});
