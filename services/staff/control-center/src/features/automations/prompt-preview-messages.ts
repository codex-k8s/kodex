export const automationPreviewMessages = {
  ru: {
    title: "Что получит исполнитель",
    draft: "Черновик",
    current: "Текущая сохранённая ревизия",
    draftHelp:
      "Проверьте будущую задачу с учётом ваших прав. После сохранения она получит новую версию, даже если поля не изменились.",
    currentHelp:
      "Так исполнитель увидит сохранённую задачу. Несохранённые изменения формы сюда не входят; доступ к полному тексту проверяется отдельно.",
    required: "Заполните название и задачу и выберите цель для предпросмотра.",
    preview: "Показать задачу для исполнителя",
    executionActor: "Исполнитель сохранённого контекста",
    futureActor: "Исполнитель будущей ревизии",
    scheduledFor: "Рассчитанный запуск",
    base: "Базовая сохранённая ревизия",
    revision: "Версия задачи",
    notSaved: "Будет назначена после сохранения",
    variables: "Переменные Автоматизации",
    savedTask: "Сохранённая задача",
    occurrences: "Ближайшие пять запусков этой версии",
  },
  en: {
    title: "Materialized automation prompt",
    draft: "Draft",
    current: "Current saved revision",
    draftHelp:
      "Preview of a future revision under your identity. A revision is assigned only after saving, even when the fields have not changed.",
    currentHelp:
      "Preview of the saved task and specification under their execution identity. Unsaved form fields are excluded; your read permissions are checked independently.",
    required: "Enter a name and task and select a target to preview.",
    preview: "Preview automation prompt",
    executionActor: "Saved context execution identity",
    futureActor: "Future revision execution identity",
    scheduledFor: "Scheduled execution",
    base: "Base saved revision",
    revision: "Prompt revision",
    notSaved: "Assigned after saving",
    variables: "Automation variables",
    savedTask: "Saved task",
    occurrences: "Next five executions for this version",
  },
};
