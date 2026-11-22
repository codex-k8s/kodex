export const kubernetesIdentitySetupMessages = {
  ru: {
    title: "Личное подключение Kubernetes",
    sharedTitle: "Сервисное подключение Kubernetes",
    identityRequirement:
      "В текущем профиле выбранной identity требуется право get Namespace kube-system для проверки кластера. Identity health не подтверждает права CRUD, exec либо доступ ко всему кластеру.",
    description:
      "Kubeconfig остаётся на вашем компьютере. В браузер передаются только его размер и SHA256; файл загружает локальный CLI напрямую в защищённый broker.",
    export:
      "1. Экспортируйте выбранный локальный context в новый закрытый файл. Не передавайте исходный kubeconfig или файл экспорта в браузер, чат либо сотруднику.",
    exportContext: "Каноническое имя context этого подключения: {value}",
    requirementsUnavailable:
      "Требования локального экспорта недоступны или изменились. Обновите подключение; новая подготовка заблокирована.",
    inspect: "2. Получите размер и SHA256 именно экспортированного файла",
    digest: "SHA256 файла",
    bytes: "Размер файла в байтах",
    begin: "Подготовить загрузку",
    admission: "3. Скачайте одноразовый допуск",
    download: "Скачать допуск для CLI",
    protect:
      "Ограничьте права файла допуска; он содержит одноразовый секрет. Не передавайте его сотруднику, в чат или аргументы команд.",
    upload: "4. Загрузите kubeconfig локальным CLI",
    refresh: "Прочитать состояние",
    cancel: "Отменить настройку",
    unknown:
      "Результат записи не подтверждён. Прочитайте состояние по исходному ключу; повторная загрузка автоматически не выполняется.",
    selector: "Найти настройку",
    setupRef: "Ссылка настройки",
    beginKey: "Ключ исходной подготовки",
    key: "Ключ или ссылка",
    recovery:
      "Повторное чтение не восстанавливает одноразовый допуск. Если он утрачен, отмените незавершённую настройку перед новой подготовкой.",
    testRequired:
      "Identity сохранена. Обновите подключение и выполните его проверку: создание identity ещё не подтверждает актуальные права Kubernetes.",
    reload: "Обновить подключение для проверки",
    identity: "Identity: {ref}, ревизия {revision}",
    expiry: "Допуск до {value}",
    states: {
      PREPARED: "Ожидает локальной загрузки",
      ATTEMPTING: "Загрузка начата",
      COMPLETED: "Требуется проверка подключения",
      CANCELLED: "Настройка отменена",
      EXPIRED: "Допуск истёк",
      UNKNOWN: "Результат загрузки неизвестен; повтор запрещён",
      ACK_REJECTED: "Ответ загрузки отклонён; требуется новая настройка",
    },
    reasons: {
      SETUP_EXPIRED_PENDING_CLOSURE:
        "Допуск истёк; сервер завершает закрытие настройки.",
      SETUP_COMPLETION_REQUIRED:
        "Необходимо сверить завершение исходной загрузки.",
      SETUP_ACK_REJECTED: "Ответ загрузки не соответствует выданному допуску.",
      SETUP_ACK_CONFLICT: "Получены противоречивые сведения о загрузке.",
    },
  },
  en: {
    title: "Personal Kubernetes connection",
    sharedTitle: "Service Kubernetes connection",
    identityRequirement:
      "The current profile requires the selected identity to get Namespace kube-system for cluster verification. Identity health does not confirm CRUD, exec or whole-cluster permissions.",
    description:
      "Kubeconfig stays on your computer. The browser receives only its size and SHA256; the local CLI uploads the file directly to the protected broker.",
    export:
      "1. Export the selected local context into a new private file. Never send the source or exported kubeconfig to the browser, chat or agent.",
    exportContext: "This connection's canonical context name: {value}",
    requirementsUnavailable:
      "Local export requirements are unavailable or changed. Refresh the connection; new setup is blocked.",
    inspect: "2. Inspect the exact exported file",
    digest: "File SHA256",
    bytes: "File size in bytes",
    begin: "Prepare upload",
    admission: "3. Download the one-time admission",
    download: "Download admission for CLI",
    protect:
      "Restrict the admission file permissions: it contains a one-time secret. Do not share it with an agent, chat or command arguments.",
    upload: "4. Upload kubeconfig using the local CLI",
    refresh: "Read setup status",
    cancel: "Cancel setup",
    unknown:
      "The mutation outcome is unconfirmed. Read status using the original key; upload is never repeated automatically.",
    selector: "Find setup",
    setupRef: "Setup reference",
    beginKey: "Original Begin key",
    key: "Key or reference",
    recovery:
      "Reading status cannot recover the one-time admission. If lost, cancel the unfinished setup before preparing another.",
    testRequired:
      "Identity was stored. Refresh and test the connection: identity creation does not confirm current Kubernetes permissions.",
    reload: "Refresh connection for testing",
    identity: "Identity: {ref}, revision {revision}",
    expiry: "Admission expires at {value}",
    states: {
      PREPARED: "Awaiting local upload",
      ATTEMPTING: "Upload started",
      COMPLETED: "Connection test required",
      CANCELLED: "Setup cancelled",
      EXPIRED: "Admission expired",
      UNKNOWN: "Upload outcome unknown; retry prohibited",
      ACK_REJECTED: "Upload acknowledgement rejected; new setup required",
    },
    reasons: {
      SETUP_EXPIRED_PENDING_CLOSURE:
        "Admission expired; the server is closing the setup.",
      SETUP_COMPLETION_REQUIRED: "Reconcile the original upload completion.",
      SETUP_ACK_REJECTED:
        "The upload acknowledgement does not match its admission.",
      SETUP_ACK_CONFLICT: "Conflicting upload facts were received.",
    },
  },
};
