import type {
  ConfigOverlaySchema,
  ProviderModelCatalogStatus,
} from "../shared/api/generated/openapi/types.gen";
// Точный пятикомпонентный owner golden HTTP runtime_overlay_schema.json; только локальная оснастка.
export const overlaySchemaFixture: ConfigOverlaySchema = {
  revision:
    "cos_e84f6b4c641531755549ee8c79f186e3f8778511afe2e63786269a776d90bceb",
  digest: "e84f6b4c641531755549ee8c79f186e3f8778511afe2e63786269a776d90bceb",
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
      key: "web_search",
      valueType: "string",
      allowedValues: ["disabled", "cached", "indexed", "live"],
      defaultValue: "cached",
      description: "Режим hosted native web search",
      completion: "web_search = ",
      hover:
        "Настройка Codex 0.160.0, не доказательство успешного вызова. Sandbox domain allowlist не применяется к hosted search; shell egress не меняется.",
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
