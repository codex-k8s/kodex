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
  };
  instructions: {
    editor: string;
    preview: string;
    markdown: string;
    saveDraft: string;
    variables: string;
    variablesUnavailable: string;
    usedVariables: string;
    noVariables: string;
    validation: string;
  };
  environment: {
    current: string;
    catalog: string;
    localSearch: string;
    choose: string;
    loadingMore: string;
    imageReady: string;
  };
  access: {
    integrationsEmpty: string;
    knowledgeBindings: string;
  };
  gaps: {
    title: string;
    description: string;
    overlay: string;
    variables: string;
    avatar: string;
    environmentSearch: string;
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
    help: "API поддерживает только ссылку на изображение.",
    generate: "Создать с Kodex",
    upload: "Загрузить изображение",
    fallback: "Используются инициалы",
  },
  profile: { save: "Сохранить профиль" },
  runtime: {
    title: "Модель и среда выполнения",
    profile: "Runtime-профиль",
    catalogRef: "Единый выбор из авторитетного runtime catalog",
    overlay: "Overlay config.toml",
    overlayHelp:
      "Чтение, проверка и сохранение overlay не представлены текущим API.",
    overlayPlaceholder: "Overlay недоступен: API чтения отсутствует",
    save: "Сохранить runtime",
  },
  instructions: {
    editor: "Редактор",
    preview: "Предпросмотр",
    markdown: "Markdown-шаблон инструкций",
    saveDraft: "Сохранить черновик",
    variables: "Template variables",
    variablesUnavailable: "Каталог разрешённых переменных не представлен API.",
    usedVariables: "Переменные в тексте",
    noVariables: "В тексте нет шаблонных переменных",
    validation: "Сообщения проверки",
  },
  environment: {
    current: "Текущее окружение",
    catalog: "Каталог окружений",
    localSearch:
      "Поиск и cursor-разбиение выполняются по уже загруженному каталогу.",
    choose: "Найти окружение по названию, назначению или ПО",
    loadingMore: "Загружаем следующую страницу",
    imageReady: "Образ подготовлен",
  },
  access: {
    integrationsEmpty: "Интеграционные grants не выданы",
    knowledgeBindings: "Привязанные источники знаний",
  },
  gaps: {
    title: "API gaps",
    description:
      "Эти зоны показаны fail-closed и не имитируют применение изменений.",
    overlay:
      "Нет операций чтения, проверки и mutation для config.toml overlay.",
    variables: "Нет каталога разрешённых template variables.",
    avatar:
      "Нет upload/generation mutation для аватара; доступен только avatarUrl.",
    environmentSearch:
      "listRoleEnvironments не поддерживает query, pageToken и серверную pagination.",
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
    help: "The API currently supports only an image URL.",
    generate: "Create with Kodex",
    upload: "Upload image",
    fallback: "Initials are used",
  },
  profile: { save: "Save profile" },
  runtime: {
    title: "Model and runtime environment",
    profile: "Runtime profile",
    catalogRef: "One selection from the authoritative runtime catalog",
    overlay: "config.toml overlay",
    overlayHelp:
      "Overlay read, validation, and save operations are absent from the current API.",
    overlayPlaceholder: "Overlay unavailable: no read API",
    save: "Save runtime",
  },
  instructions: {
    editor: "Editor",
    preview: "Preview",
    markdown: "Instruction Markdown template",
    saveDraft: "Save draft",
    variables: "Template variables",
    variablesUnavailable:
      "The API does not provide an allowed variable catalog.",
    usedVariables: "Variables used in text",
    noVariables: "The text does not use template variables",
    validation: "Validation messages",
  },
  environment: {
    current: "Current environment",
    catalog: "Environment catalog",
    localSearch:
      "Search and cursor paging run over the catalog already loaded by the client.",
    choose: "Find by name, purpose, or software",
    loadingMore: "Loading the next page",
    imageReady: "Image prepared",
  },
  access: {
    integrationsEmpty: "No integration grants",
    knowledgeBindings: "Bound knowledge sources",
  },
  gaps: {
    title: "API gaps",
    description:
      "These areas fail closed and never simulate successful application.",
    overlay:
      "No read, validate, or mutation operations for config.toml overlay.",
    variables: "No allowed template variable catalog.",
    avatar:
      "No avatar upload or generation mutation; only avatarUrl is available.",
    environmentSearch:
      "listRoleEnvironments has no query, pageToken, or server pagination.",
  },
};

export function agentDetailCopy(locale: string): AgentDetailCopy {
  return locale.toLocaleLowerCase().startsWith("ru") ? ru : en;
}
