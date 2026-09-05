import type {
  ConfigOverlaySchema,
  ProviderModelCatalogStatus,
} from "../shared/api/generated/openapi/types.gen";
// Точный owner golden из HTTP checkpoint 5d09619a; только локальная оснастка.
export const overlaySchemaFixture: ConfigOverlaySchema = {
  revision:
    "cos_03fe45f9f298bce8f9866a55bbc1bfecdb34858fc53b2fb51b7c73ba79745830",
  digest: "03fe45f9f298bce8f9866a55bbc1bfecdb34858fc53b2fb51b7c73ba79745830",
  fields: [
    {
      key: "model_reasoning_effort",
      valueType: "string",
      allowedValues: ["low", "high"],
      defaultValue: "high",
      description: "Степень рассуждения выбранной модели",
      completion: "model_reasoning_effort = ",
      hover:
        "Допустимые значения определяются exact каталогами выбранных provider accounts.",
    },
    {
      key: "personality",
      valueType: "string",
      allowedValues: ["none", "friendly", "pragmatic"],
      defaultValue: "",
      description: "Стиль ответов",
      completion: "personality = ",
      hover: "Не изменяет полномочия или ограничения runtime.",
    },
    {
      key: "allow_login_shell",
      valueType: "boolean",
      allowedValues: ["false"],
      defaultValue: "false",
      description: "Запрет login shell",
      completion: "allow_login_shell = false",
      hover: "Разрешено только false.",
    },
    {
      key: "history.persistence",
      valueType: "string",
      allowedValues: ["save-all", "none"],
      defaultValue: "save-all",
      description: "Сохранение истории",
      completion: "history.persistence = ",
      hover: "save-all сохраняет историю; none отключает её сохранение.",
    },
  ],
  maximumBytes: 65536,
};
export const catalogStatusFixture: ProviderModelCatalogStatus = {
  state: "READY",
  observedAt: "2026-09-05T00:00:00Z",
  expiresAt: "2099-01-01T00:00:00Z",
  source: "REMOTE_API",
  failure: "NONE",
};
