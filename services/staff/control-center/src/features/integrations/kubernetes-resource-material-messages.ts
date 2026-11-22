export const kubernetesResourceMaterialMessages = {
  ru: {
    title: "Защищённые данные Secret",
    description:
      "Значения Secret остаются в локальном файле. Браузер принимает только назначение, SHA256 и размер; локальный CLI загружает данные напрямую в защищённый broker. Ни один шаг подготовки не изменяет Secret в Kubernetes.",
    destination: "Точное назначение защищённого материала",
    operation: "Операция",
    namespace: "Namespace Secret",
    name: "Имя Secret",
    generateName: "Префикс generateName (только create вместо имени)",
    secretType: "Тип Secret",
    patchType: "Диалект patch",
    notPatch: "Не patch",
    inspect: "Получите безопасные метаданные локального JSON-файла",
    patchInstructions:
      "Для patch используйте --patch-file <local-path> --patch-type JSON|MERGE|STRATEGIC --namespace <namespace> --name <name> --secret-type <type> вместо --secret-file. Передайте эти же параметры при загрузке. Содержимое файла не вставляйте в браузер, чат или аргументы команд.",
    digest: "SHA256 защищённого содержимого",
    bytes: "Размер защищённого содержимого в байтах",
    begin: "Подготовить допуск загрузки",
    download: "Скачать допуск для локального CLI",
    protect:
      "Файл допуска содержит одноразовый секрет. Сохраните его только на своём компьютере и ограничьте права; не передавайте сотруднику или в чат.",
    unknown:
      "Результат записи неизвестен. Прочитайте состояние по исходному ключу; повторная запись автоматически не выполняется.",
    reference: "Ссылка материала: {ref}",
    expiry: "Текущий срок материала: {value}",
    cancel: "Отозвать будущую возможность использования",
    recovery:
      "Чтение не восстанавливает одноразовый допуск. UNKNOWN не доказывает отсутствие загрузки; новая подготовка не отменяет прежнее действие.",
    selector: "Найти защищённый материал",
    materialRef: "Ссылка материала",
    beginKey: "Ключ исходной подготовки",
    key: "Ссылка материала или ключ подготовки",
    refresh: "Прочитать состояние материала",
    states: {
      PREPARED: "Ожидает локальной загрузки",
      ATTEMPTING: "Загрузка начата",
      AVAILABLE:
        "Защищённый материал подготовлен; изменение Secret требует отдельного допуска",
      UNKNOWN: "Результат загрузки неизвестен; повтор запрещён",
      ACK_REJECTED: "Ответ загрузки отклонён",
      CANCELLED: "Будущее использование отозвано",
      EXPIRED: "Срок материала истёк",
      CONSUMED:
        "Материал использован; результат изменения проверяется отдельно",
    },
    reasons: {
      MATERIAL_EXPIRED_PENDING_CLOSURE:
        "Срок истёк; сервер завершает закрытие материала.",
      MATERIAL_COMPLETION_REQUIRED:
        "Необходимо сверить завершение исходной загрузки.",
      MATERIAL_ACK_REJECTED: "Ответ загрузки не соответствует допуску.",
      MATERIAL_ACK_CONFLICT: "Получены противоречивые сведения о загрузке.",
      MATERIAL_SOURCE_CHANGED: "Источник или выбранное подключение изменились.",
      MATERIAL_CANCELLED: "Будущее использование материала отозвано.",
      MATERIAL_CONSUMED: "Материал уже использован.",
      MATERIAL_UNAVAILABLE: "Материал недоступен для использования.",
      MATERIAL_OBSERVATION_ABSENT:
        "Наблюдение пока отсутствует; это не подтверждает отсутствие загрузки.",
    },
  },
  en: {
    title: "Protected Secret data",
    description:
      "Secret values stay in a local file. The browser accepts only destination, SHA256 and size; the local CLI uploads directly to the protected broker. Preparation never changes a Kubernetes Secret.",
    destination: "Exact protected material destination",
    operation: "Operation",
    namespace: "Secret namespace",
    name: "Secret name",
    generateName: "generateName prefix (create instead of name)",
    secretType: "Secret type",
    patchType: "Patch dialect",
    notPatch: "Not a patch",
    inspect: "Inspect safe metadata from the local JSON file",
    patchInstructions:
      "For patch, use --patch-file <local-path> --patch-type JSON|MERGE|STRATEGIC --namespace <namespace> --name <name> --secret-type <type> instead of --secret-file. Use the same parameters for upload. Never paste file contents into the browser, chat or command arguments.",
    digest: "Protected content SHA256",
    bytes: "Protected content size in bytes",
    begin: "Prepare upload admission",
    download: "Download admission for local CLI",
    protect:
      "The admission file contains a one-time secret. Keep it on your computer with restricted permissions; never share it with an agent or chat.",
    unknown:
      "The mutation outcome is unknown. Read status using the original key; no mutation is retried automatically.",
    reference: "Material reference: {ref}",
    expiry: "Current material deadline: {value}",
    cancel: "Revoke future use",
    recovery:
      "Reading cannot recover the one-time admission. UNKNOWN does not prove absence; preparing new material does not undo a previous action.",
    selector: "Find protected material",
    materialRef: "Material reference",
    beginKey: "Original Begin key",
    key: "Material reference or original Begin key",
    refresh: "Read status",
    states: {
      PREPARED: "Awaiting local upload",
      ATTEMPTING: "Upload started",
      AVAILABLE:
        "Protected material is available; changing the Secret requires separate admission",
      UNKNOWN: "Upload outcome unknown; retry prohibited",
      ACK_REJECTED: "Upload acknowledgement rejected",
      CANCELLED: "Future use revoked",
      EXPIRED: "Material expired",
      CONSUMED: "Material consumed; target effect is checked separately",
    },
    reasons: {
      MATERIAL_EXPIRED_PENDING_CLOSURE: "Expired; owner closure is pending.",
      MATERIAL_COMPLETION_REQUIRED: "Reconcile the original upload completion.",
      MATERIAL_ACK_REJECTED: "Upload acknowledgement does not match admission.",
      MATERIAL_ACK_CONFLICT: "Conflicting upload facts were received.",
      MATERIAL_SOURCE_CHANGED: "Source or selected connection changed.",
      MATERIAL_CANCELLED: "Future use was revoked.",
      MATERIAL_CONSUMED: "Material was already consumed.",
      MATERIAL_UNAVAILABLE: "Material is unavailable for use.",
      MATERIAL_OBSERVATION_ABSENT:
        "Observation is not yet available; this does not prove upload absence.",
    },
  },
};
