import { createTestStorage as storage } from "@/test-utils/storage";
import { describe, expect, it } from "vitest";

import {
  consumeRuntimeSecretReauthSuggestion,
  rememberRuntimeSecretReauthSuggestion,
  runtimeSecretReauthSuggestionStorageKey,
} from "./reauth-suggestion";

const suggestion = {
  name: "SERVICE_AUTH",
  description: "Доступ к сервису",
  valueType: "STRING" as const,
  sourceHelp: "Получите значение в кабинете сервиса.",
};

describe("runtime Secret re-auth suggestion", () => {
  it("одноразово восстанавливает только безопасные метаданные", () => {
    const target = storage();
    rememberRuntimeSecretReauthSuggestion(
      target,
      "project_sales",
      suggestion,
      "assistant",
      1000,
    );
    const serialized = target.getItem(runtimeSecretReauthSuggestionStorageKey);
    expect(serialized).not.toContain("secret-value");
    expect(
      consumeRuntimeSecretReauthSuggestion(
        target,
        { projectRef: "project_sales", surface: "assistant" },
        1100,
      ),
    ).toEqual(suggestion);
    expect(
      consumeRuntimeSecretReauthSuggestion(
        target,
        { projectRef: "project_sales", surface: "assistant" },
        1100,
      ),
    ).toBeUndefined();
  });

  it("отклоняет чужой контекст и просроченную запись", () => {
    const wrongProject = storage();
    rememberRuntimeSecretReauthSuggestion(
      wrongProject,
      "project_sales",
      suggestion,
      "assistant",
      1000,
    );
    expect(
      consumeRuntimeSecretReauthSuggestion(
        wrongProject,
        { projectRef: "project_other", surface: "assistant" },
        1100,
      ),
    ).toBeUndefined();

    const expired = storage();
    rememberRuntimeSecretReauthSuggestion(
      expired,
      "project_sales",
      suggestion,
      undefined,
      1000,
    );
    expect(
      consumeRuntimeSecretReauthSuggestion(
        expired,
        { projectRef: "project_sales" },
        301001,
      ),
    ).toBeUndefined();
  });
});
