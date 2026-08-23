import { describe, expect, it } from "vitest";

import { isAgentDraftComplete } from "@/features/platform/agent-form";

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
});
