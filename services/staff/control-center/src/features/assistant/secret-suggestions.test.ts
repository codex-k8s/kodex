import { describe, expect, it } from "vitest";

import { parseAssistantSecretSuggestions } from "@/features/assistant/secret-suggestions";

describe("подсказки для защищённой формы секрета", () => {
  it("принимает только безопасные метаданные", () => {
    expect(
      parseAssistantSecretSuggestions([
        {
          name: "SERVICE_AUTH",
          valueType: "STRING",
          sourceHelp: "Создайте ключ в кабинете сервиса.",
        },
      ]),
    ).toEqual([
      {
        name: "SERVICE_AUTH",
        description: "",
        valueType: "STRING",
        sourceHelp: "Создайте ключ в кабинете сервиса.",
      },
    ]);
    expect(parseAssistantSecretSuggestions(undefined)).toEqual([]);
  });

  it("отклоняет неверные подсказки", () => {
    for (const input of [
      [
        {
          name: "SERVICE_AUTH",
          valueType: "STRING",
          sourceHelp: "Кабинет",
          value: "forged",
        },
      ],
      [
        {
          name: "SERVICE_AUTH",
          valueType: "STRING",
          sourceHelp: "Кабинет",
          secretValue: "forged",
        },
      ],
      [{ name: "SERVICE_AUTH", valueType: "FILE", sourceHelp: "Кабинет" }],
      [{ name: "SERVICE_AUTH", valueType: "STRING", sourceHelp: "" }],
      [
        { name: "SERVICE_AUTH", valueType: "STRING", sourceHelp: "Кабинет" },
        { name: "SERVICE_AUTH", valueType: "JSON", sourceHelp: "Кабинет" },
      ],
    ]) {
      expect(parseAssistantSecretSuggestions(input)).toBeUndefined();
    }
  });
});
