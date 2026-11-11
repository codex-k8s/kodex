import type {
  ConfigOverlaySchema,
  ProviderModelCatalogStatus,
} from "../shared/api/generated/openapi/types.gen";
// Каноническая схема OpenCode от runtimecontract.OverlaySchema; синтетическая оснастка.
export const overlaySchemaFixture: ConfigOverlaySchema = {
  revision:
    "cos_2c42a753c49d4ef469936d1ee3809528033235d9b0f5e15d2959b03134750ede",
  digest: "2c42a753c49d4ef469936d1ee3809528033235d9b0f5e15d2959b03134750ede",
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
      key: "context.auto_compaction",
      valueType: "boolean",
      allowedValues: ["true", "false"],
      defaultValue: "",
      description: "Штатное автоматическое сжатие OpenCode",
      completion: "context.auto_compaction = true",
      hover:
        "Если поле отсутствует, используется настройка маршрута; по умолчанию включено.",
    },
    {
      key: "context.prune",
      valueType: "boolean",
      allowedValues: ["true", "false"],
      defaultValue: "",
      description: "Штатное сокращение результатов инструментов в контексте",
      completion: "context.prune = true",
      hover: "Не удаляет исходный audit и не меняет retention сессии Kodex.",
    },
    {
      key: "cache.optimization",
      valueType: "string",
      allowedValues: ["PROVIDER_DEFAULT", "NO_HINTS", "OPTIMIZE"],
      defaultValue: "",
      description: "Намерение оптимизации inference cache",
      completion: "cache.optimization = ",
      hover:
        "NO_HINTS убирает подсказки Kodex, но не доказывает отключение кеша провайдера.",
    },
    {
      key: "cache.reuse_scope",
      valueType: "string",
      allowedValues: ["PROJECT", "INITIATOR", "SESSION"],
      defaultValue: "",
      description: "Область допустимого повторного использования",
      completion: "cache.reuse_scope = ",
      hover:
        "Фактическая гарантия зависит от квалифицированного маршрута. Обязательную область маршрута нельзя расширять.",
    },
    {
      key: "cache.no_reuse",
      valueType: "boolean",
      allowedValues: ["true", "false"],
      defaultValue: "",
      description: "Обязательный запрет межзапросного reuse",
      completion: "cache.no_reuse = true",
      hover:
        "При true неподдержанный маршрут отклоняется до запроса. Это не гарантия NO_STORE.",
    },
    {
      key: "cache.no_store",
      valueType: "boolean",
      allowedValues: ["true", "false"],
      defaultValue: "",
      description:
        "Обязательный запрет сохранения переиспользуемого inference cache",
      completion: "cache.no_store = true",
      hover:
        "Неподтверждённая гарантия закрыто отклоняется; не означает отсутствие рабочих KV текущего запроса.",
    },
    {
      key: "cache.required",
      valueType: "boolean",
      allowedValues: ["true", "false"],
      defaultValue: "",
      description: "Обязательность выбранной оптимизации",
      completion: "cache.required = true",
      hover:
        "NO_REUSE и NO_STORE всегда обязательны. Ограничения маршрута нельзя ослабить.",
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
