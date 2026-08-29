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
    generate: string;
    upload: string;
    remove: string;
    fallback: string;
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
    help: "Аватар загружается как файл. Ручной ввод URL не используется; операция станет доступна после появления avatar asset API.",
    generate: "Создать с Kodex",
    upload: "Загрузить изображение",
    remove: "Удалить аватар",
    fallback: "Используются инициалы",
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
      "Нет upload/remove mutation для avatar asset; сохранённое изображение доступно только для чтения.",
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
    help: "The avatar is uploaded as a file. Manual URL input is not used; the operation will become available with the avatar asset API.",
    generate: "Create with Kodex",
    upload: "Upload image",
    remove: "Remove avatar",
    fallback: "Initials are used",
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
      "There are no upload/remove mutations for avatar assets; a stored image is read-only.",
  },
};

export function agentDetailCopy(locale: string): AgentDetailCopy {
  return locale.toLocaleLowerCase().startsWith("ru") ? ru : en;
}
