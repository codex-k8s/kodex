export const personalEmailMessages = {
  ru: {
    staleCredential:
      "Старая попытка относится к другому поколению владельца. Её секрет мог быть сохранён ранее, но не подходит текущему владельцу. Можно удалить только локальные сведения о попытке и настроить реквизит заново.",
    discardStaleCredential: "Начать настройку нового поколения",
    title: "Моя почта",
    description:
      "Отдельный личный ящик. Пароль хранится в почтовом подключении; обычный ключ интеграции здесь не используется.",
    refresh: "Обновить",
    search: "Название подключения",
    find: "Найти",
    name: "Название",
    sender: "Адрес отправителя",
    create: "Создать личное почтовое подключение",
    empty: "У вас пока нет почтовых подключений.",
    loading: "Проверяем ваши почтовые подключения…",
    open: "Настроить ящик",
    next: "Показать ещё",
    created:
      "Подключение создано. Откройте его настройку, добавьте реквизиты и опубликуйте конфигурацию.",
    noRights:
      "Создание не выдаёт права на проекты или агентов. Доступ агента к ящику настраивается отдельным разрешением.",
    pending:
      "Результат записи не подтверждён. Сначала проверьте квитанцию. Если она пока отсутствует, обновите список; явный повтор возможен только с прежними полями. Автоматической повторной записи нет.",
    recover: "Проверить квитанцию",
    refreshRequired: "Перед продолжением обновите состояние подключения.",
    personal: "Личный ящик инициатора",
    service: "Явная сервисная учётная запись",
    available: "Готов к проверке разрешений запуска",
    notReady: "Настройка или владелец пока недоступны",
    states: {
      UNCONFIGURED: "Владелец не настроен",
      ACTIVE: "Владелец назначен",
      REVOKED: "Доступ отозван",
    },
    ownerTitle: "Владелец почтовых реквизитов",
    ownerHelp:
      "Инициатор запуска должен иметь право на этот ящик. Утверждение действия другим человеком не меняет владельца реквизитов.",
    ownerUnavailable:
      "Владелец либо конфигуратор недоступен. Текущее сохранённое состояние не разрешает новые действия.",
    policy: "Режим владения",
    serviceRef: "Сервисная учётная запись",
    serviceHelp:
      "Сервисный режим требует отдельного права управления организацией и активной сервисной учётной записи. Личный доступ к своему подключению этого права не даёт.",
    idle: "Для назначения или замены владельца отключите подключение, его разрешения и завершите активные процессы. После смены нужны новые реквизиты и новая публикация.",
    configure: "Назначить владельца",
    revoke: "Отозвать доступ ящика",
    revokeHelp:
      "Отзыв блокирует новые действия. Уже начатая отправка могла произойти; её история и UNKNOWN не превращаются в успех или отменённую отправку.",
    saved: "Запись подтверждена; читаем текущее состояние владельца.",
    test: "Проверить SMTP/IMAP",
    testHelp:
      "Проверка использует текущего владельца и опубликованную конфигурацию. Она не отправляет письмо и не подтверждает разрешение отправки.",
    profile: "Профиль почтового провайдера",
    profiles: {
      CUSTOM: "Свои адреса SMTP/IMAP",
      TIMEWEB_PASSWORD: "Timeweb — пароль почтового ящика",
      GMAIL_APP_PASSWORD: "Gmail — пароль приложения",
    },
    gmailHelp:
      "Нужен разрешённый аккаунтом пароль приложения и двухэтапная проверка. Обычный пароль Google не подходит. Этот профиль не настраивает OAuth/XOAUTH2 или обновление OAuth-токенов.",
    timewebHelp:
      "Укажите полный адрес почты как username и пароль ящика. SMTP/IMAP работают только через проверяемый TLS.",
    policyHelp:
      "Чтение — без отдельного согласования; черновики, флаги и перенос — в ограниченной области согласия; отправка и удаление сообщений — с подтверждением каждого действия. DENY запрещает операцию, HUMAN_GATE требует отдельного подтверждения и для остальных операций.",
    applyProfile: "Заполнить политики выбранного профиля",
    profilePolicy: "По профилю согласования",
  },
  en: {
    staleCredential:
      "The old attempt belongs to another owner generation. Its secret may have been stored earlier but is ineligible for the current owner. You can remove only its local attempt metadata and set up a new credential.",
    discardStaleCredential: "Set up the new owner generation",
    title: "My email",
    description:
      "A separate personal mailbox. Its password belongs to the mailbox connection; a generic integration token is not used.",
    refresh: "Refresh",
    search: "Connection name",
    find: "Find",
    name: "Name",
    sender: "Sender address",
    create: "Create personal mailbox connection",
    empty: "You have no mailbox connections yet.",
    loading: "Checking your mailbox connections…",
    open: "Set up mailbox",
    next: "Show more",
    created:
      "Connection created. Open its settings, add credentials and publish its configuration.",
    noRights:
      "Creation grants no project or agent permissions. Agent access to a mailbox requires a separate grant.",
    pending:
      "The write is unconfirmed. Check its receipt first. If no receipt is available yet, refresh the list; an explicit retry requires the original fields. Writes are never retried automatically.",
    recover: "Check receipt",
    refreshRequired: "Refresh the connection state before continuing.",
    personal: "Initiator's personal mailbox",
    service: "Explicit service identity",
    available: "Ready for launch permission checks",
    notReady: "Setup or owner is unavailable",
    states: {
      UNCONFIGURED: "Owner not configured",
      ACTIVE: "Owner assigned",
      REVOKED: "Access revoked",
    },
    ownerTitle: "Mailbox credential owner",
    ownerHelp:
      "The root initiator must be eligible for this mailbox. Approval by someone else does not change credential ownership.",
    ownerUnavailable:
      "The owner or configuring user is unavailable. Stored active state does not authorize new actions.",
    policy: "Ownership mode",
    serviceRef: "Service identity",
    serviceHelp:
      "Service mode requires separate organization management permission and an active service identity. Access to your own connection grants neither.",
    idle: "Disable the connection and its grants and finish active processes before assigning or replacing the owner. New credentials and a new publication are required afterward.",
    configure: "Assign owner",
    revoke: "Revoke mailbox access",
    revokeHelp:
      "Revocation blocks new actions. An already started send may have happened; its history and UNKNOWN outcome never become a successful or cancelled send.",
    saved: "Write confirmed; reading current ownership state.",
    test: "Check SMTP/IMAP",
    testHelp:
      "The check uses the current owner and published configuration. It sends no email and grants no permission to send.",
    profile: "Email provider profile",
    profiles: {
      CUSTOM: "Custom SMTP/IMAP endpoints",
      TIMEWEB_PASSWORD: "Timeweb — mailbox password",
      GMAIL_APP_PASSWORD: "Gmail — app password",
    },
    gmailHelp:
      "Requires an account-permitted app password and two-step verification. A normal Google password is unsupported. This profile does not configure OAuth/XOAUTH2 or OAuth token refresh.",
    timewebHelp:
      "Use the full email address as username and the mailbox password. SMTP/IMAP require verified TLS.",
    policyHelp:
      "Reads need no separate approval; drafts, flags and moves use scoped consent; sending and message deletion require approval for each action. DENY blocks an operation; HUMAN_GATE additionally requires individual approval for other operations.",
    applyProfile: "Fill policies for the selected profile",
    profilePolicy: "According to approval profile",
  },
};
