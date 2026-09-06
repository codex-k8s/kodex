// Дополнение существующего реестра именами точных application permissions.
export const additionalPermissionTranslations = {
  "agent.avatar.manage": [
    "Изменять аватары сотрудников",
    "Загружать и заменять аватары разрешённых сотрудников.",
    "Manage employee avatars",
    "Upload and replace avatars of allowed employees.",
  ],
  "artifact.bind": [
    "Изменять связи файлов",
    "Добавлять и снимать связи доступных файлов с сотрудниками.",
    "Manage file bindings",
    "Add and remove employee bindings for accessible files.",
  ],
  "artifact.delete": [
    "Перемещать файлы в корзину",
    "Удалять доступные файлы с возможностью восстановления.",
    "Move files to trash",
    "Delete accessible files while retaining recovery.",
  ],
  "artifact.purge": [
    "Удалять файлы безвозвратно",
    "Безвозвратно удалять доступные файлы из корзины.",
    "Permanently delete files",
    "Permanently remove accessible files from trash.",
  ],
  "artifact.restore": [
    "Восстанавливать файлы",
    "Восстанавливать доступные файлы из корзины.",
    "Restore files",
    "Restore accessible files from trash.",
  ],
  "artifact.upload": [
    "Загружать файлы",
    "Добавлять файлы в разрешённую область.",
    "Upload files",
    "Add files within the allowed scope.",
  ],
  "environment.privileged.manage": [
    "Управлять привилегированными окружениями",
    "Настраивать привилегированный доступ разрешённых окружений с повторным подтверждением.",
    "Manage privileged environments",
    "Configure privileged access for allowed environments with reauthentication.",
  ],
  "image.build": [
    "Собирать образы ролей",
    "Запускать сборки разрешённых образов ролей.",
    "Build role images",
    "Start builds of allowed role images.",
  ],
  "image.source.view": [
    "Читать исходники образов ролей",
    "Просматривать Dockerfile и установочные блоки разрешённых образов ролей.",
    "Read role image sources",
    "View Dockerfiles and installation blocks of allowed role images.",
  ],
  "image.source.manage": [
    "Изменять исходники образов ролей",
    "Создавать и изменять исходники разрешённых образов ролей при наличии права сборки.",
    "Manage role image sources",
    "Create and edit allowed role image sources with build permission.",
  ],
  "image.promote": [
    "Публиковать образы ролей",
    "Запрашивать допуск проверенных образов к использованию.",
    "Promote role images",
    "Request promotion of verified images for use.",
  ],
  "platform.stt.use": [
    "Использовать голосовой ввод",
    "Преобразовывать речь в текст через настроенный системный сервис.",
    "Use voice input",
    "Transcribe speech through the configured system service.",
  ],
  "prompt.full.view": [
    "Просматривать полный промпт",
    "Читать разрешённое полное представление материализованного промпта.",
    "View full prompts",
    "Read the authorized full materialized prompt projection.",
  ],
  "provider.account.authorize": [
    "Авторизовать учётные записи провайдера",
    "Подтверждать доступ разрешённых учётных записей к провайдеру.",
    "Authorize provider accounts",
    "Authorize allowed provider accounts.",
  ],
  "provider.account.manage": [
    "Управлять учётными записями провайдера",
    "Создавать и изменять разрешённые учётные записи провайдера.",
    "Manage provider accounts",
    "Create and update allowed provider accounts.",
  ],
  "provider.account.revoke": [
    "Отзывать учётные записи провайдера",
    "Прекращать использование разрешённых учётных записей провайдера.",
    "Revoke provider accounts",
    "Stop use of allowed provider accounts.",
  ],
  "provider.account.view": [
    "Просматривать учётные записи провайдера",
    "Читать безопасные сведения о доступных учётных записях провайдера.",
    "View provider accounts",
    "Read safe details of accessible provider accounts.",
  ],
  "run.cancel": [
    "Отменять запуски",
    "Отменять разрешённые запуски независимо от их инициатора.",
    "Cancel runs",
    "Cancel allowed runs regardless of their initiator.",
  ],
  "runtime.environment.delete": [
    "Удалять окружения",
    "Удалять разрешённые окружения с проверкой действующих зависимостей.",
    "Delete environments",
    "Delete allowed environments subject to active dependency checks.",
  ],
  "runtime.environment.disable": [
    "Отключать окружения",
    "Прекращать использование разрешённых окружений для новых запусков.",
    "Disable environments",
    "Stop use of allowed environments for new runs.",
  ],
  "secret.create": [
    "Создавать секреты",
    "Добавлять секреты в разрешённый Проект.",
    "Create secrets",
    "Add secrets to an allowed Project.",
  ],
  "secret.reveal": [
    "Просматривать значения секретов",
    "Однократно раскрывать разрешённое значение секрета после подтверждения.",
    "Reveal secret values",
    "Reveal an allowed secret value after confirmation.",
  ],
  "secret.revoke": [
    "Отзывать секреты",
    "Прекращать использование разрешённых секретов.",
    "Revoke secrets",
    "Stop use of allowed secrets.",
  ],
  "secret.rotate": [
    "Обновлять секреты",
    "Создавать и публиковать новые ревизии разрешённых секретов.",
    "Rotate secrets",
    "Create and publish new revisions of allowed secrets.",
  ],
  "secret.view": [
    "Просматривать сведения о секретах",
    "Читать безопасные метаданные доступных секретов без значений.",
    "View secret metadata",
    "Read safe metadata of accessible secrets without their values.",
  ],
  "session.cancel": [
    "Отменять сессии",
    "Прекращать разрешённые сессии и связанное с ними исполнение.",
    "Cancel sessions",
    "Stop allowed sessions and their associated execution.",
  ],
} as const;

export const serverPermissionKeys = [
  "access.manage",
  "access.view",
  "agent.avatar.manage",
  "agent.launch",
  "agent.manage",
  "agent.view",
  "artifact.bind",
  "artifact.delete",
  "artifact.download",
  "artifact.purge",
  "artifact.restore",
  "artifact.upload",
  "artifact.view",
  "audit.view",
  "environment.privileged.manage",
  "gate.resolve",
  "image.build",
  "image.source.view",
  "image.source.manage",
  "image.promote",
  "integration.manage",
  "integration.view",
  "organization.manage",
  "organization.view",
  "platform.stt.use",
  "project.create",
  "project.manage",
  "project.view",
  "prompt.full.view",
  "provider.account.authorize",
  "provider.account.manage",
  "provider.account.revoke",
  "provider.account.view",
  "run.cancel",
  "run.cancel.own",
  "run.view",
  "runtime.environment.delete",
  "runtime.environment.disable",
  "schedule.manage",
  "schedule.view",
  "secret.create",
  "secret.reveal",
  "secret.revoke",
  "secret.rotate",
  "secret.view",
  "session.cancel",
  "workflow.launch",
  "workflow.manage",
  "workflow.view",
] as const;

type AdditionalPermissionKey = keyof typeof additionalPermissionTranslations;
export type ServerPermissionKey = (typeof serverPermissionKeys)[number];
export type PermissionLabel = { name: string; description: string };

export function additionalPermissionMessages(locale: "ru" | "en") {
  const offset = locale === "ru" ? 0 : 2;
  return Object.fromEntries(
    Object.entries(additionalPermissionTranslations).map(([key, values]) => [
      key,
      {
        name: values[offset],
        description: values[offset + 1],
      },
    ]),
  ) as Record<AdditionalPermissionKey, PermissionLabel>;
}

export const serverPermissionTokens = Object.fromEntries(
  serverPermissionKeys.flatMap((key) =>
    (["name", "description"] as const).map((field) => [
      `PERMISSION_${key.replaceAll(".", "_").toUpperCase()}_${field.toUpperCase()}`,
      [key, field] as const,
    ]),
  ),
) as Record<string, readonly [ServerPermissionKey, keyof PermissionLabel]>;
