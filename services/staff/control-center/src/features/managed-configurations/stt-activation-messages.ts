export const sttActivationMessages = {
  ru: {
    activation: {
      title: "Активная конфигурация распознавания",
      active: "Сейчас активна «{name}», ревизия {revision}.",
      intro:
        "Публикация сохраняет ревизию. Для применения к распознаванию активируйте её отдельным действием.",
      prepare: "Подготовить активацию",
      confirm: "Активировать эту ревизию",
      cancel: "Отменить выбор",
      first:
        "Активной конфигурации пока нет. Будет создана первая привязка распознавания.",
      change:
        "Сменить «{name}», ревизия {revision}, на выбранную конфигурацию.",
      target: "Будет применена «{name}», ревизия {revision}.",
      ready: "Активная конфигурация готова к распознаванию.",
      notReady:
        "Конфигурация активна, но распознавание пока не готово. Проверьте провайдера, учётные данные и доступ к распознаванию.",
      absent: "Активная конфигурация отсутствует.",
      unknown:
        "Исход команды неизвестен. Повторная отправка заблокирована. Проверьте фактическое состояние; закрытие экрана не отменяет команду.",
      readback: "Проверить активную конфигурацию",
      stale: "Данные изменились. Обновите редактор и заново подтвердите выбор.",
      acknowledged:
        "Команда принята. Ниже показано повторное чтение активной конфигурации.",
      observed:
        "Выбранная ревизия сейчас активна. Это чтение состояния, а не квитанция исходного запроса.",
      already: "Выбранная ревизия уже активна.",
    },
  },
  en: {
    activation: {
      title: "Active speech recognition configuration",
      active: "Currently active: “{name}”, revision {revision}.",
      intro:
        "Publishing saves a revision. Activate it separately to use it for speech recognition.",
      prepare: "Prepare activation",
      confirm: "Activate this revision",
      cancel: "Cancel selection",
      first:
        "There is no active configuration. This creates the first speech recognition binding.",
      change:
        "Replace “{name}”, revision {revision}, with the selected configuration.",
      target: "Apply “{name}”, revision {revision}.",
      ready: "The active configuration is ready for speech recognition.",
      notReady:
        "The configuration is active, but speech recognition is not ready. Check the provider, credential, and speech recognition access.",
      absent: "There is no active configuration.",
      unknown:
        "The command outcome is unknown. Resending is blocked. Check the actual state; closing this screen does not cancel the command.",
      readback: "Check active configuration",
      stale:
        "The data changed. Refresh the editor and confirm a new selection.",
      acknowledged:
        "The command was accepted. The active configuration was read again below.",
      observed:
        "The selected revision is currently active. This is a state observation, not a receipt for the original request.",
      already: "The selected revision is already active.",
    },
  },
};
