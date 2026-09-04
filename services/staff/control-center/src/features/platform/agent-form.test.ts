import { describe, expect, it } from "vitest";

import {
  isAgentDraftComplete,
  resolveAgentRuntimeRef,
} from "@/features/platform/agent-form";

describe("agent form", () => {
  const complete = {
    name: "Аналитик продаж",
    purpose: "Квалифицировать входящие обращения",
    roleDescription: "Проверяет факты и отмечает допущения",
    initialInstructions: "Отвечай по-русски и не выдумывай данные",
    runtimeRef: "runtime_standard",
  };

  it("разрешает отправку полностью заполненной формы", () => {
    expect(isAgentDraftComplete(complete)).toBe(true);
  });

  it.each(Object.keys(complete) as Array<keyof typeof complete>)(
    "не отправляет форму без поля %s",
    (field) => {
      expect(isAgentDraftComplete({ ...complete, [field]: "   " })).toBe(false);
    },
  );

  it("выбирает первый runtime после поздней загрузки каталога", () => {
    expect(resolveAgentRuntimeRef("", [])).toBe("");
    expect(resolveAgentRuntimeRef("", ["runtime_standard"])).toBe(
      "runtime_standard",
    );
  });

  it("сохраняет доступный выбор и заменяет недоступный", () => {
    expect(
      resolveAgentRuntimeRef("runtime_custom", [
        "runtime_standard",
        "runtime_custom",
      ]),
    ).toBe("runtime_custom");
    expect(
      resolveAgentRuntimeRef("runtime_removed", ["runtime_standard"]),
    ).toBe("runtime_standard");
  });
});
