import { describe, expect, it } from "vitest";

import {
  tokenizeAssistantCodeLine,
  validateAssistantObjectJSON,
} from "@/features/assistant/code-editor";

describe("assistant code editor model", () => {
  it("подсвечивает ключи и значения JSON раздельно", () => {
    const tokens = tokenizeAssistantCodeLine(
      '  "enabled": true, "retries": 3',
      "json",
    );

    expect(tokens.map((item) => item.tone)).toEqual([
      "plain",
      "key",
      "plain",
      "keyword",
      "plain",
      "key",
      "plain",
      "number",
    ]);
  });

  it("принимает только JSON-объект для параметров плана", () => {
    expect(validateAssistantObjectJSON('{"name":"Продажи"}')).toBe(true);
    expect(validateAssistantObjectJSON("[]")).toBe(false);
    expect(validateAssistantObjectJSON("not-json")).toBe(false);
  });
});
