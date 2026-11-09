export const approvalConsentMessages = {
  ru: {
    title: "Мои временные разрешения",
    description:
      "Согласования конкретных объектов, выданные для ваших запусков. Отзыв закрывает новые действия; уже начатое действие может завершиться.",
    choiceTitle: "На сколько разрешить действия в этой области",
    choiceHelp:
      "Объект указан выше. Первый вызов входит в лимит. Разрешение не расширяет права ключа, сотрудника или подключения.",
    duration: "Срок в секундах (86400 = сутки)",
    effects: "Максимальное число вызовов",
    lifetime: "Где действует",
    lifetimes: {
      ROOT_RUN: "Только этот запуск",
      INITIATOR_RESOURCE: "Мои запуски этого сотрудника для того же объекта",
    },
    invalid: "Укажите целый срок и лимит в допустимых пределах.",
    refresh: "Обновить разрешения",
    revoke: "Отозвать разрешение",
    more: "Показать ещё",
    empty: "Временных разрешений пока нет",
    remaining: "Зарезервировано {used} из {total} вызовов",
    expires: "Действует до {at}",
    states: {
      ACTIVE: "Действует",
      EXPIRED: "Срок истёк",
      EXHAUSTED: "Лимит исчерпан",
      REVOKED: "Отозвано",
    },
  },
  en: {
    title: "My temporary approvals",
    description:
      "Consents for specific objects in your runs. Revocation blocks new actions; an already admitted action may still complete.",
    choiceTitle: "Approve actions within this scope",
    choiceHelp:
      "The object is shown above. The first call counts toward the limit. Approval does not expand credential, agent or connection permissions.",
    duration: "Duration in seconds (86400 = one day)",
    effects: "Maximum calls",
    lifetime: "Applies to",
    lifetimes: {
      ROOT_RUN: "This run only",
      INITIATOR_RESOURCE: "My runs of this agent for the same object",
    },
    invalid:
      "Choose an integer duration and call limit within the policy bounds.",
    refresh: "Refresh approvals",
    revoke: "Revoke approval",
    more: "Show more",
    empty: "No temporary approvals yet",
    remaining: "{used} of {total} calls reserved",
    expires: "Expires at {at}",
    states: {
      ACTIVE: "Active",
      EXPIRED: "Expired",
      EXHAUSTED: "Exhausted",
      REVOKED: "Revoked",
    },
  },
};
