export interface AgentDetailCopy {
  apply: {
    title: string;
    apiReadback: string;
    localDraft: string;
    saving: string;
    failed: string;
    boundaries: Record<"next-run" | "next-turn" | "published", string>;
  };
  avatar: {
    preview: string;
    help: string;
    upload: string;
    remove: string;
    fallback: string;
    cropTitle: string;
    cropCanvas: string;
    cropHelp: string;
    cropApply: string;
    zoom: string;
    processingError: string;
    dimensionError: string;
    typeError: string;
    removeTitle: string;
    removeConfirmation: string;
  };
  profile: {
    save: string;
  };
  runtime: {
    title: string;
    profile: string;
    catalogRef: string;
    overlay: string;
    overlayHelp: string;
    overlayPlaceholder: string;
    save: string;
    saveOverlay: string;
    accountPolicy: string;
    accounts: string;
    unavailableSelection: string;
  };
  instructions: {
    editor: string;
    preview: string;
    markdown: string;
    saveDraft: string;
    variables: string;
    variablesHelp: string;
    variableSearch: string;
    variableScope: string;
    variableExample: string;
    collection: string;
    insertVariable: string;
    usedVariables: string;
    noVariables: string;
    validation: string;
  };
  environment: {
    current: string;
    catalog: string;
    localSearch: string;
    serverSearch: string;
    choose: string;
    loadingMore: string;
    imageReady: string;
    bind: string;
    values: string;
    secrets: string;
  };
  access: {
    integrationsEmpty: string;
    knowledgeBindings: string;
  };
  gaps: {
    title: string;
    description: string;
    avatar: string;
  };
}

const ru: AgentDetailCopy = {
  apply: {
    title: "Применение изменений",
    apiReadback: "Подтверждено ответом API",
    localDraft: "Локальные изменения ещё не отправлены",
    saving: "Ожидается авторитетный ответ API",
    failed: "Изменения не применены",
    boundaries: {
      "next-run": "Будет использовано в следующем запуске",
      "next-turn": "Будет использовано в следующем ходе через RuntimeRevision",
      published: "Runtime получает только опубликованную версию",
    },
  },
  avatar: {
    preview: "Аватар сотрудника",
    help: "Изображение обрезается до квадрата и сохраняется как новая неизменяемая ревизия файла Проекта.",
    upload: "Загрузить изображение",
    remove: "Удалить аватар",
    fallback: "Используются инициалы",
    cropTitle: "Настроить аватар",
    cropCanvas: "Квадратная область обрезки аватара",
    cropHelp:
      "Перемещайте изображение мышью или стрелками. Масштаб изменяется ползунком.",
    cropApply: "Обрезать и загрузить",
    zoom: "Масштаб",
    processingError: "Не удалось обработать изображение.",
    dimensionError:
      "Размер изображения превышает безопасный предел 8192 × 8192.",
    typeError: "Выберите JPEG, PNG или WebP размером не более 10 МБ.",
    removeTitle: "Удалить аватар?",
    removeConfirmation:
      "Ссылка будет удалена из профиля, а файл перемещён в корзину Проекта на 30 дней.",
  },
  profile: { save: "Сохранить профиль" },
  runtime: {
    title: "Модель и среда выполнения",
    profile: "Runtime-профиль",
    catalogRef: "Единый выбор из авторитетного runtime catalog",
    overlay: "Overlay config.toml",
    overlayHelp:
      "Черновик проверяется сервером и применяется только после публикации.",
    overlayPlaceholder: "# Параметры, разрешённые политикой Kodex",
    save: "Сохранить runtime",
    saveOverlay: "Сохранить overlay",
    accountPolicy: "Политика аккаунтов",
    accounts: "Аккаунты",
    unavailableSelection: "Текущий выбор недоступен",
  },
  instructions: {
    editor: "Редактор",
    preview: "Предпросмотр",
    markdown: "Markdown-шаблон инструкций",
    saveDraft: "Сохранить черновик",
    variables: "Template variables",
    variablesHelp:
      "Авторитетный каталог сгруппирован по scope. Выбор вставляет переменную в позицию курсора.",
    variableSearch: "Найти переменную по имени или описанию",
    variableScope: "Scope",
    variableExample: "Пример",
    collection: "Коллекция",
    insertVariable: "Вставить переменную",
    usedVariables: "Переменные в тексте",
    noVariables: "В тексте нет шаблонных переменных",
    validation: "Сообщения проверки",
  },
  environment: {
    current: "Текущее окружение",
    catalog: "Каталог окружений",
    localSearch:
      "Поиск и cursor-разбиение выполняются по уже загруженному каталогу.",
    serverSearch:
      "Поиск и cursor pagination выполняются авторитетным API окружений.",
    choose: "Найти окружение по названию, назначению или ПО",
    loadingMore: "Загружаем следующую страницу",
    imageReady: "Образ подготовлен",
    bind: "Назначить окружение",
    values: "Переменные окружения",
    secrets: "Ссылки на секреты",
  },
  access: {
    integrationsEmpty: "Интеграционные grants не выданы",
    knowledgeBindings: "Привязанные источники знаний",
  },
  gaps: {
    title: "Ограничения API",
    description:
      "Эти зоны показаны fail-closed и не имитируют применение изменений.",
    avatar:
      "Для изменения аватара нужны права на профиль сотрудника и файлы Проекта.",
  },
};

const en: AgentDetailCopy = {
  apply: {
    title: "Applying changes",
    apiReadback: "Confirmed by the API response",
    localDraft: "Local changes have not been submitted",
    saving: "Waiting for the authoritative API response",
    failed: "Changes were not applied",
    boundaries: {
      "next-run": "Will be used by the next run",
      "next-turn": "Will be used by the next turn via RuntimeRevision",
      published: "Runtime only receives a published version",
    },
  },
  avatar: {
    preview: "Employee avatar",
    help: "The image is cropped to a square and stored as a new immutable Project file revision.",
    upload: "Upload image",
    remove: "Remove avatar",
    fallback: "Initials are used",
    cropTitle: "Adjust avatar",
    cropCanvas: "Square avatar crop area",
    cropHelp:
      "Move the image with the pointer or arrow keys. Use the slider to zoom.",
    cropApply: "Crop and upload",
    zoom: "Zoom",
    processingError: "The image could not be processed.",
    dimensionError: "The image exceeds the safe 8192 × 8192 limit.",
    typeError: "Select a JPEG, PNG, or WebP image up to 10 MB.",
    removeTitle: "Remove avatar?",
    removeConfirmation:
      "The profile link will be cleared and the file will move to the Project trash for 30 days.",
  },
  profile: { save: "Save profile" },
  runtime: {
    title: "Model and runtime environment",
    profile: "Runtime profile",
    catalogRef: "One selection from the authoritative runtime catalog",
    overlay: "config.toml overlay",
    overlayHelp:
      "The draft is validated by the server and only applies after publication.",
    overlayPlaceholder: "# Parameters allowed by the Kodex policy",
    save: "Save runtime",
    saveOverlay: "Save overlay",
    accountPolicy: "Account policy",
    accounts: "Accounts",
    unavailableSelection: "Current selection is unavailable",
  },
  instructions: {
    editor: "Editor",
    preview: "Preview",
    markdown: "Instruction Markdown template",
    saveDraft: "Save draft",
    variables: "Template variables",
    variablesHelp:
      "The authoritative catalog is grouped by scope. Selecting an item inserts it at the cursor.",
    variableSearch: "Find a variable by name or description",
    variableScope: "Scope",
    variableExample: "Example",
    collection: "Collection",
    insertVariable: "Insert variable",
    usedVariables: "Variables used in text",
    noVariables: "The text does not use template variables",
    validation: "Validation messages",
  },
  environment: {
    current: "Current environment",
    catalog: "Environment catalog",
    localSearch:
      "Search and cursor paging run over the catalog already loaded by the client.",
    serverSearch:
      "Search and cursor pagination are provided by the authoritative environment API.",
    choose: "Find by name, purpose, or software",
    loadingMore: "Loading the next page",
    imageReady: "Image prepared",
    bind: "Assign environment",
    values: "Environment values",
    secrets: "Secret descriptors",
  },
  access: {
    integrationsEmpty: "No integration grants",
    knowledgeBindings: "Bound knowledge sources",
  },
  gaps: {
    title: "API gaps",
    description:
      "These areas fail closed and never simulate successful application.",
    avatar:
      "Changing the avatar requires access to the employee profile and Project files.",
  },
};

export function agentDetailCopy(locale: string): AgentDetailCopy {
  return locale.toLocaleLowerCase().startsWith("ru") ? ru : en;
}
