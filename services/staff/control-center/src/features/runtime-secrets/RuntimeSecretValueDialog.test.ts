import { expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { createI18n } from "vue-i18n";

import { captureSetupState } from "@/test-utils/setup-harness";
import RuntimeSecretValueDialog from "./RuntimeSecretValueDialog.vue";

vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));

it("подсказка помощника заполняет только метаданные, значение остаётся пустым", async () => {
  const state = (await captureSetupState(
    RuntimeSecretValueDialog,
    (app) => app.use(createI18n({ legacy: false, locale: "ru" })),
    {
      suggestion: {
        name: "SERVICE_AUTH",
        description: "Доступ к сервису",
        valueType: "JSON",
        sourceHelp: "Получите значение в кабинете сервиса.",
      },
    },
  )) as unknown as {
    name: Ref<string>;
    description: Ref<string>;
    valueType: Ref<string>;
    value: Ref<string>;
  };
  expect(state.name.value).toBe("SERVICE_AUTH");
  expect(state.description.value).toBe("Доступ к сервису");
  expect(state.valueType.value).toBe("JSON");
  expect(state.value.value).toBe("");
});
