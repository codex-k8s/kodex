import { describe, expect, it } from "vitest";

import {
  presentRuntimeText,
  runtimeProgressKey,
} from "@/features/runs/runtime-text";

const identity = (value: string): string => value;

describe("presentRuntimeText", () => {
  it("распознаёт только известные статусы выполнения для локализации", () => {
    expect(runtimeProgressKey("WORKLOAD_SCHEDULED")).toBe(
      "runs.runtimeProgress.workloadScheduled",
    );
    expect(runtimeProgressKey("MODEL_REQUEST_RUNNING")).toBe(
      "runs.runtimeProgress.modelRequestRunning",
    );
    expect(runtimeProgressKey("ARBITRARY_INTERNAL_STATUS")).toBeUndefined();
  });
  it("скрывает служебные коды и внутренние ссылки runtime", () => {
    expect(
      presentRuntimeText(
        "MODEL_REQUEST_RUNNING run_secret_internal_reference",
        identity,
      ),
    ).toBeUndefined();
  });

  it.each([
    "USER_MESSAGE",
    "ASSISTANT_MESSAGE",
    "INTERMEDIATE_MESSAGE",
    "FINAL_MESSAGE",
  ] as const)("сохраняет полезный текст %s", (messageKind) => {
    expect(
      presentRuntimeText("KODEX_CONTINUATION_RESULT_OK", identity, messageKind),
    ).toBe("`KODEX_CONTINUATION_RESULT_OK`");
  });

  it("не оборачивает повторно уже размеченный машинный токен", () => {
    expect(
      presentRuntimeText("`KODEX_RESULT_OK`", identity, "FINAL_MESSAGE"),
    ).toBe("`KODEX_RESULT_OK`");
  });

  it("не показывает внутреннюю ссылку внутри ответа агента", () => {
    expect(
      presentRuntimeText(
        "Готово: run_secret_internal_reference",
        identity,
        "FINAL_MESSAGE",
      ),
    ).toBe("Готово");
  });
});
