import {
  serverPermissionTokens,
  type PermissionLabel,
  type ServerPermissionKey,
} from "./permission-message-catalog";

// Закрытый реестр безопасных сообщений владельца состояния: ru, en.
export const serverTokenTranslations = {
  RUNTIME_LEASE_EXPIRED: [
    "Срок аренды исполнения истёк",
    "Runtime lease expired",
  ],
  SYSTEM_ROLE_IMAGE_SOURCE_ACCESS: [
    "Доступ к исходникам образов ролей",
    "Role image source access",
  ],
  RUNTIME_CLAIM_ELIGIBILITY_CHANGED: [
    "Запуск остановлен: изменились необходимые разрешения или зависимости",
    "Run stopped: required permissions or dependencies changed",
  ],
  PROVIDER_ACCOUNT_DELETION_REQUESTED: [
    "Удаление учётной записи провайдера запрошено",
    "Provider account deletion requested",
  ],
  PROVIDER_ACCOUNT_VERIFICATION_REQUESTED: [
    "Запрошена проверка текущего credential",
    "Current credential verification requested",
  ],
  PROVIDER_ACCOUNT_QUEUED_WORK_PROCESSED: [
    "Выбранные задания очереди обработаны",
    "Selected queued work processed",
  ],
  AUTOMATION_VARIABLE_NAME: ["Название Автоматизации", "Automation name"],
  AUTOMATION_VARIABLE_TASK: [
    "Исходная задача Автоматизации",
    "Original automation task",
  ],
  AUTOMATION_VARIABLE_SCHEDULED_AT: [
    "Рассчитанное время запуска",
    "Scheduled execution time",
  ],
  AUTOMATION_VARIABLE_TIMEZONE: [
    "Часовой пояс Автоматизации",
    "Automation timezone",
  ],
  AUTOMATION_VARIABLE_REVISION: [
    "Сохранённая неизменяемая ревизия Автоматизации",
    "Saved immutable automation revision",
  ],
  GATE_CONSEQUENCE_CONTINUE: [
    "Разрешить продолжение работы.",
    "Allow work to continue.",
  ],
  GATE_CONSEQUENCE_EXTERNAL_EFFECT: [
    "Разрешить указанное внешнее действие после повторной проверки полномочий. Результат будет подтверждён отдельной квитанцией.",
    "Allow the specified external action after a fresh authority check. A separate receipt will confirm its result.",
  ],
  GATE_CONSEQUENCE_REJECT_RUN: [
    "Отклонить результат и завершить запуск с ошибкой.",
    "Reject the result and finish the run with an error.",
  ],
  GATE_CONSEQUENCE_REJECT_EFFECT: [
    "Отклонить только внешнее действие. Запуск не завершается.",
    "Reject only the external action. The run does not terminate.",
  ],
  GATE_CONSEQUENCE_CANCEL_RUN: ["Отменить запуск.", "Cancel the run."],
  GATE_CONSEQUENCE_CANCEL_EFFECT: [
    "Отменить только внешнее действие. Запуск не завершается.",
    "Cancel only the external action. The run does not terminate.",
  ],
  GATE_CONSEQUENCE_REQUEST_CHANGES: [
    "Продолжить предыдущий шаг с замечаниями, без немедленного внешнего действия.",
    "Continue the previous step with feedback, without an immediate external action.",
  ],
  RUNTIME_ARTIFACT_READ: [
    "Прочитан файл рабочего окружения",
    "Runtime file read",
  ],
  RUNTIME_ENVIRONMENT_IMPACT_PREPARED: [
    "Подготовлен план обновления потребителей окружения",
    "Environment consumer update plan prepared",
  ],
  AGENT_INSTRUCTIONS_IMPACT_PREPARED: [
    "Подготовлен план обновления потребителей инструкций",
    "Instruction consumer update plan prepared",
  ],
  PROMPT_TEMPLATE_IMPACT_PREPARED: [
    "Подготовлен план обновления потребителей шаблона",
    "Prompt template consumer update plan prepared",
  ],
  CAPABILITY_AGENT_MANAGE_NAME: [
    "Управление сотрудниками",
    "Employee management",
  ],
  CAPABILITY_AGENT_MANAGE_DESCRIPTION: [
    "Создание и изменение сотрудников в разрешённой области",
    "Create and update employees within the allowed scope",
  ],
  CAPABILITY_ARTIFACT_MANAGE_NAME: ["Файлы и знания", "Files and knowledge"],
  CAPABILITY_ARTIFACT_MANAGE_DESCRIPTION: [
    "Работа с файлами и связями знаний в разрешённой области",
    "Work with files and knowledge bindings within the allowed scope",
  ],
  CAPABILITY_GATE_RESOLVE_NAME: ["Принятие решений", "Approval decisions"],
  CAPABILITY_GATE_RESOLVE_DESCRIPTION: [
    "Обработка доступных запросов подтверждения",
    "Resolve accessible approval requests",
  ],
  CAPABILITY_INTEGRATION_GRANT_NAME: [
    "Разрешения интеграций",
    "Integration grants",
  ],
  CAPABILITY_INTEGRATION_GRANT_DESCRIPTION: [
    "Управление разрешёнными подключениями интеграций",
    "Manage authorized integration connections",
  ],
  CAPABILITY_PROJECT_MANAGE_NAME: [
    "Управление Проектами",
    "Project management",
  ],
  CAPABILITY_PROJECT_MANAGE_DESCRIPTION: [
    "Изменение доступных Проектов",
    "Update accessible Projects",
  ],
  CAPABILITY_RUN_DELEGATE_NAME: ["Делегирование задач", "Task delegation"],
  CAPABILITY_RUN_DELEGATE_DESCRIPTION: [
    "Передача задач разрешённым сотрудникам и Процессам",
    "Delegate tasks to allowed employees and workflows",
  ],
  CAPABILITY_RUN_LAUNCH_NAME: ["Запуск задач", "Task execution"],
  CAPABILITY_RUN_LAUNCH_DESCRIPTION: [
    "Запуск разрешённых сотрудников и Процессов",
    "Launch allowed employees and workflows",
  ],
  CAPABILITY_SCHEDULE_MANAGE_NAME: [
    "Управление автоматизациями",
    "Automation management",
  ],
  CAPABILITY_SCHEDULE_MANAGE_DESCRIPTION: [
    "Изменение доступных автоматизаций",
    "Update accessible automations",
  ],
  SYSTEM_ROLE_OWNER_DESCRIPTION: [
    "Системная роль владельца организации",
    "System role for the organization owner",
  ],
  SYSTEM_ROLE_ADMINISTRATOR_DESCRIPTION: [
    "Системная роль администратора",
    "System administrator role",
  ],
  SYSTEM_ROLE_OPERATOR_DESCRIPTION: [
    "Системная роль оператора",
    "System operator role",
  ],
  SYSTEM_ROLE_MEMBER_DESCRIPTION: [
    "Системная роль участника",
    "System member role",
  ],
  SYSTEM_ROLE_AUDITOR_DESCRIPTION: [
    "Системная роль аудитора",
    "System auditor role",
  ],
  PROVIDER_AUTH_UNAVAILABLE: [
    "Авторизация провайдера недоступна",
    "Provider authorization unavailable",
  ],
  PROVIDER_AUTH_REJECTED: [
    "Провайдер отклонил авторизацию",
    "Provider rejected authorization",
  ],
  PROVIDER_UNAVAILABLE: ["Провайдер недоступен", "Provider unavailable"],
  PROVIDER_RATE_LIMITED: [
    "Достигнут лимит запросов провайдера",
    "Provider rate limit reached",
  ],
  PROVIDER_REQUEST_REJECTED: [
    "Провайдер отклонил запрос",
    "Provider rejected the request",
  ],
  PROVIDER_RESPONSE_INVALID: [
    "Провайдер вернул некорректный ответ",
    "Provider returned an invalid response",
  ],
  PROVIDER_EMPTY_RESULT: [
    "Провайдер не вернул результат",
    "Provider returned no result",
  ],
  PROVIDER_TOOL_INVALID: [
    "Провайдер запросил недопустимый инструмент",
    "Provider requested an invalid tool",
  ],
  PROVIDER_TOOL_LIMIT: [
    "Достигнут лимит вызовов инструментов",
    "Tool call limit reached",
  ],
  RUNTIME_PROFILE_UNSUPPORTED: [
    "Профиль исполнения не поддерживается",
    "Runtime profile is unsupported",
  ],
  RUNTIME_INPUT_INVALID: [
    "Входные данные исполнения недопустимы",
    "Runtime input is invalid",
  ],
  RUNTIME_INPUT_TOO_LARGE: [
    "Входные данные превышают допустимый размер",
    "Runtime input exceeds the size limit",
  ],
  RUNTIME_MCP_UNAVAILABLE: [
    "Инструменты исполнения недоступны",
    "Runtime tools unavailable",
  ],
  RUNTIME_UNAVAILABLE: [
    "Исполнение временно недоступно",
    "Runtime temporarily unavailable",
  ],
  RUNTIME_LIMIT_EXCEEDED: [
    "Достигнут лимит исполнения",
    "Runtime limit exceeded",
  ],
  INTEGRATION_AUTH_REJECTED: [
    "Интеграция отклонила авторизацию",
    "Integration rejected authorization",
  ],
  INTEGRATION_CREDENTIAL_UNAVAILABLE: [
    "Учётные данные интеграции недоступны",
    "Integration credentials unavailable",
  ],
  INTEGRATION_UNAVAILABLE: ["Интеграция недоступна", "Integration unavailable"],
  INTEGRATION_RATE_LIMITED: [
    "Достигнут лимит запросов интеграции",
    "Integration rate limit reached",
  ],
  INTEGRATION_CONFIGURATION_INVALID: [
    "Конфигурация интеграции недопустима",
    "Integration configuration is invalid",
  ],
  INTEGRATION_CAPABILITY_UNSUPPORTED: [
    "Действие не поддерживается интеграцией",
    "Integration capability is unsupported",
  ],
  INTEGRATION_ROUTE_NOT_OWNED: [
    "Маршрут интеграции не принадлежит текущей области",
    "Integration route is outside the current ownership scope",
  ],
  INTEGRATION_REQUEST_REJECTED: [
    "Интеграция отклонила запрос",
    "Integration rejected the request",
  ],
  INTEGRATION_RESPONSE_INVALID: [
    "Интеграция вернула некорректный ответ",
    "Integration returned an invalid response",
  ],
  INTEGRATION_OUTCOME_UNKNOWN: [
    "Исход операции интеграции неизвестен",
    "Integration outcome is unknown",
  ],
  ACCESS_BINDING_CHANGED: [
    "Правило доступа изменено",
    "Access binding changed",
  ],
  ACCESS_ROLE_CHANGED: ["Роль доступа изменена", "Access role changed"],
  AGENT_AVATAR_UPDATED: [
    "Аватар сотрудника обновлён",
    "Employee avatar updated",
  ],
  AGENT_CONFIG_OVERLAY_CHANGED: [
    "Дополнительная конфигурация сотрудника изменена",
    "Employee configuration overlay changed",
  ],
  AGENT_CONTEXT_BINDING_CHANGED: [
    "Контекст сотрудника изменён",
    "Employee context binding changed",
  ],
  AGENT_CREATED_READY: ["Сотрудник создан", "Employee created"],
  AGENT_INSTRUCTIONS_UPDATED: [
    "Инструкции сотрудника обновлены",
    "Employee instructions updated",
  ],
  AGENT_PERMISSIONS_UPDATED: [
    "Разрешения сотрудника обновлены",
    "Employee permissions updated",
  ],
  AGENT_RUNTIME_CONFIGURATION_PUBLISHED: [
    "Конфигурация исполнения сотрудника опубликована",
    "Employee runtime configuration published",
  ],
  AGENT_RUNTIME_ENVIRONMENT_BOUND: [
    "Окружение назначено сотруднику",
    "Environment assigned to the employee",
  ],
  AGENT_UPDATED: ["Сотрудник обновлён", "Employee updated"],
  ARTIFACT_AVAILABLE: ["Файл доступен", "File available"],
  ARTIFACT_BINDING_UPDATED: ["Связь файла обновлена", "File binding updated"],
  ARTIFACT_DELETED: ["Файл перемещён в корзину", "File moved to trash"],
  ARTIFACT_DOWNLOADED: ["Файл скачан", "File downloaded"],
  ARTIFACT_PREVIEWED: ["Открыт просмотр файла", "File preview opened"],
  ARTIFACT_PURGED: ["Файл удалён безвозвратно", "File permanently deleted"],
  ARTIFACT_RESTORED: ["Файл восстановлен", "File restored"],
  ARTIFACT_UPLOADED: ["Файл загружен", "File uploaded"],
  ASSISTANT_CONVERSATION_ARCHIVED: [
    "Диалог архивирован",
    "Conversation archived",
  ],
  ASSISTANT_CONVERSATION_CREATED: ["Диалог создан", "Conversation created"],
  ASSISTANT_CONVERSATION_TITLE_UPDATED: [
    "Название диалога обновлено",
    "Conversation title updated",
  ],
  ASSISTANT_EXECUTING: [
    "Помощник выполняет задачу",
    "Assistant is executing the task",
  ],
  ASSISTANT_INSTRUCTIONS_UPDATED: [
    "Инструкции помощника обновлены",
    "Assistant instructions updated",
  ],
  ASSISTANT_PLAN_APPLIED: ["План помощника применён", "Assistant plan applied"],
  ASSISTANT_PLAN_CONFLICT: [
    "План конфликтует с текущим состоянием",
    "Plan conflicts with the current state",
  ],
  ASSISTANT_PLAN_DRAFT_UPDATED: [
    "Черновик плана обновлён",
    "Plan draft updated",
  ],
  ASSISTANT_PLAN_PROPOSED: [
    "Помощник предложил план",
    "Assistant proposed a plan",
  ],
  ASSISTANT_PLAN_REJECTED: [
    "План помощника отклонён",
    "Assistant plan rejected",
  ],
  ASSISTANT_PLAN_VALIDATED: [
    "План помощника проверен",
    "Assistant plan validated",
  ],
  ASSISTANT_READY: ["Помощник готов", "Assistant ready"],
  ASSISTANT_RECOVERING: [
    "Помощник восстанавливается",
    "Assistant is recovering",
  ],
  ASSISTANT_RECOVERY_REQUESTED: [
    "Запрошено восстановление помощника",
    "Assistant recovery requested",
  ],
  ASSISTANT_TURN_ACCEPTED: [
    "Сообщение принято помощником",
    "Assistant message accepted",
  ],
  ASSISTANT_TURN_QUEUED: ["Сообщение поставлено в очередь", "Message queued"],
  ATTACHMENT_SET_DRAFT_CREATED: [
    "Черновик вложений создан",
    "Attachment draft created",
  ],
  ATTACHMENT_SET_DRAFT_REVISED: [
    "Черновик вложений обновлён",
    "Attachment draft updated",
  ],
  ATTACHMENT_SET_FINALIZED: ["Вложения подготовлены", "Attachments finalized"],
  CALLBACK_CONTINUATION_QUEUED: [
    "Продолжение по результату поставлено в очередь",
    "Result callback continuation queued",
  ],
  CHILD_AGENT_RESULT_DELIVERED: [
    "Результат дочернего сотрудника доставлен",
    "Child employee result delivered",
  ],
  CHILD_AGENT_STARTED: ["Дочерний сотрудник запущен", "Child employee started"],
  CHILD_CALLBACK_REGISTERED: [
    "Ожидание результата дочернего запуска зарегистрировано",
    "Child result callback registered",
  ],
  CHILD_RUN_DELEGATED: [
    "Задача передана дочернему запуску",
    "Task delegated to a child run",
  ],
  CONFIG_OVERLAY_INVALID_OR_PROTECTED: [
    "Конфигурация содержит ошибку или защищённое поле",
    "Configuration contains an invalid or protected field",
  ],
  DEFAULT_PROVIDER_ACCOUNT_NAME: [
    "Основная учётная запись провайдера",
    "Default provider account",
  ],
  DEFAULT_RUNTIME_ENVIRONMENT: ["Основное окружение", "Default environment"],
  DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION: [
    "Окружение исполнения по умолчанию",
    "Default runtime environment",
  ],
  DEFAULT_RUNTIME_NAME: ["Основное исполнение", "Default runtime"],
  EMAIL_EFFECT_RECONCILED: [
    "Решение по почтовой операции зафиксировано",
    "Email effect reconciliation recorded",
  ],
  EMAIL_EFFECT_REPORTED: [
    "Исход почтовой операции зарегистрирован",
    "Email effect outcome recorded",
  ],
  EMAIL_MAILBOX_CREDENTIAL_CREATED: [
    "Учётные данные почты сохранены",
    "Mailbox credential saved",
  ],
  EMAIL_MAILBOX_GIT_PUBLICATION_PENDING: [
    "Почтовая конфигурация из Git ожидает применения",
    "Git mailbox configuration is awaiting application",
  ],
  EMAIL_MAILBOX_GIT_SOURCE_RECORDED: [
    "Источник почтовой конфигурации записан",
    "Mailbox configuration source recorded",
  ],
  EMAIL_MAILBOX_PUBLICATION_FAILED: [
    "Не удалось применить почтовую конфигурацию",
    "Mailbox configuration application failed",
  ],
  EMAIL_MAILBOX_PUBLICATION_PENDING: [
    "Почтовая конфигурация ожидает применения",
    "Mailbox configuration is awaiting application",
  ],
  EMAIL_MAILBOX_PUBLICATION_READY: [
    "Почтовая конфигурация применена",
    "Mailbox configuration applied",
  ],
  INSTRUCTIONS_TOO_SHORT: [
    "Добавьте более подробные инструкции",
    "Provide more detailed instructions",
  ],
  INTEGRATION_ACTION_COMPLETED: [
    "Действие интеграции завершено",
    "Integration action completed",
  ],
  INTEGRATION_CONNECTION_CREATED: ["Подключение создано", "Connection created"],
  INTEGRATION_CONNECTION_DELETED: ["Подключение удалено", "Connection deleted"],
  INTEGRATION_CONNECTION_TEST_COMPLETED: [
    "Проверка подключения завершена",
    "Connection test completed",
  ],
  INTEGRATION_CONNECTION_UPDATED: [
    "Подключение обновлено",
    "Connection updated",
  ],
  INTEGRATION_CREDENTIAL_CONFIGURED: [
    "Учётные данные настроены",
    "Credentials configured",
  ],
  INTEGRATION_CREDENTIAL_INVALID: [
    "Учётные данные недействительны",
    "Credentials are invalid",
  ],
  INTEGRATION_CREDENTIAL_NOT_CONFIGURED: [
    "Учётные данные не настроены",
    "Credentials are not configured",
  ],
  INTEGRATION_EFFECT_GATE_NODE_NAME: [
    "Подтверждение внешнего действия",
    "External action approval",
  ],
  INTEGRATION_EFFECT_GATE_PROMPT: [
    "Проверьте последствия внешнего действия и примите решение",
    "Review the external action effects and make a decision",
  ],
  INTEGRATION_EFFECT_GATE_TITLE: [
    "Решение по внешнему действию",
    "External action decision",
  ],
  INTEGRATION_EFFECT_OWNER_DECISION_REQUIRED: [
    "Внешнее действие требует решения владельца",
    "External action requires an owner decision",
  ],
  INTEGRATION_GRANT_UPDATED: [
    "Разрешение интеграции обновлено",
    "Integration grant updated",
  ],
  INTEGRATION_INVOCATION_COMPLETED: [
    "Вызов интеграции завершён",
    "Integration invocation completed",
  ],
  INTEGRATION_TEST_SUCCEEDED: [
    "Подключение проверено успешно",
    "Connection test succeeded",
  ],
  INTERACTION_AUTHORITY_CHANGED: [
    "Подключение или разрешения изменены. Отправка отменена.",
    "The connection or permissions changed. Delivery was cancelled.",
  ],
  INTERACTION_DELIVERY_APPROVAL_REQUIRED: [
    "Для доставки результата требуется решение владельца.",
    "Owner approval is required to deliver the result.",
  ],
  INTERACTION_DELIVERY_GATE_PROMPT: [
    "Разрешить доставку результата запуска в указанный внешний канал?",
    "Allow delivery of the run result to the specified external channel?",
  ],
  INTERACTION_DELIVERY_GATE_TITLE: [
    "Подтверждение доставки во внешний канал",
    "External channel delivery approval",
  ],
  INTERACTION_DELIVERY_FAILED: [
    "Не удалось доставить сообщение",
    "Message delivery failed",
  ],
  INTERACTION_DELIVERY_OUTCOME_UNKNOWN: [
    "Исход доставки неизвестен",
    "Delivery outcome is unknown",
  ],
  INTERACTION_DELIVERY_RECONCILIATION_REQUIRED: [
    "Требуется сверить исход доставки",
    "Delivery outcome requires reconciliation",
  ],
  INTERACTION_DELIVERY_RECOVERED: [
    "Доставка восстановлена",
    "Delivery recovered",
  ],
  INTERACTION_DELIVERY_RECOVERY_COMPLETE: [
    "Восстановление доставки завершено",
    "Delivery recovery completed",
  ],
  INTERACTION_DELIVERY_RETRYING: [
    "Повторная попытка доставки",
    "Retrying delivery",
  ],
  INTERACTION_DELIVERY_RETRY_EXHAUSTED: [
    "Попытки доставки исчерпаны",
    "Delivery retries exhausted",
  ],
  INTERACTION_DELIVERY_SUCCEEDED: ["Сообщение доставлено", "Message delivered"],
  INTERACTION_IDENTITY_CHANGED: [
    "Привязка внешней учётной записи изменена",
    "External identity binding changed",
  ],
  MANAGED_CONFIGURATION_CHANGED: [
    "Управляемая конфигурация изменена",
    "Managed configuration changed",
  ],
  MATTERMOST_EVENT_ALREADY_PROCESSED: [
    "Событие Mattermost уже обработано",
    "Mattermost event already processed",
  ],
  MATTERMOST_GATE_COMMAND_HELP: [
    "Выберите допустимое решение для запроса подтверждения",
    "Choose an allowed decision for the approval request",
  ],
  MATTERMOST_GATE_RESOLVED: [
    "Решение из Mattermost принято",
    "Mattermost decision accepted",
  ],
  MATTERMOST_GATE_STALE: [
    "Запрос подтверждения больше не актуален",
    "Approval request is no longer current",
  ],
  MATTERMOST_INBOUND_ROUTE_UNAVAILABLE: [
    "Маршрут входящего сообщения недоступен",
    "Inbound message route is unavailable",
  ],
  MATTERMOST_INBOUND_RUN: ["Запуск из Mattermost", "Run from Mattermost"],
  MATTERMOST_RUN_ACCEPTED: [
    "Запуск из Mattermost принят",
    "Run from Mattermost accepted",
  ],
  MEMORY_RECORD_CHANGED: ["Запись памяти изменена", "Memory record changed"],
  NEW_ASSISTANT_CONVERSATION: ["Новый диалог", "New conversation"],
  OIDC_USER_NAME: ["Пользователь", "User"],
  ONBOARDING_COMPLETED: [
    "Начальная настройка завершена",
    "Initial setup completed",
  ],
  OWNER_CHANGES_QUEUED: [
    "Изменения владельца поставлены в очередь",
    "Owner changes queued",
  ],
  OWNER_DECISION_REQUIRED: [
    "Требуется решение владельца",
    "Owner decision required",
  ],
  OWNER_GATE_CANCELLED: [
    "Запрос решения отменён",
    "Decision request cancelled",
  ],
  OWNER_GATE_NODE_NAME: ["Решение владельца", "Owner decision"],
  OWNER_GATE_NODE_ROLE: ["Подтверждение владельцем", "Owner approval"],
  OWNER_GATE_RESOLVED: ["Решение владельца принято", "Owner decision accepted"],
  OWNER_GATE_REVIEW_PROMPT: [
    "Проверьте результат и примите решение",
    "Review the result and make a decision",
  ],
  OWNER_GATE_REVIEW_TITLE: [
    "Проверка результата владельцем",
    "Owner result review",
  ],
  PLATFORM_ACCESS_UPDATED: [
    "Доступ к платформе обновлён",
    "Platform access updated",
  ],
  PROJECT_ACCESS_UPDATED: [
    "Доступ к Проекту обновлён",
    "Project access updated",
  ],
  PROJECT_CREATED: ["Проект создан", "Project created"],
  PROJECT_UPDATED: ["Проект обновлён", "Project updated"],
  PROVIDER_ACCOUNT_AUTHORIZED: [
    "Учётная запись провайдера авторизована",
    "Provider account authorized",
  ],
  PROVIDER_ACCOUNT_CREATED: [
    "Учётная запись провайдера создана",
    "Provider account created",
  ],
  PROVIDER_ACCOUNT_REVOKED: [
    "Учётная запись провайдера отозвана",
    "Provider account revoked",
  ],
  PROVIDER_ACCOUNT_UPDATED: [
    "Учётная запись провайдера обновлена",
    "Provider account updated",
  ],
  PROVIDER_AUTHORIZATION_FAILED: [
    "Авторизация провайдера завершилась ошибкой",
    "Provider authorization failed",
  ],
  PROVIDER_CREDENTIAL_REFRESH_COMMITTED: [
    "Обновление учётных данных провайдера сохранено",
    "Provider credential refresh committed",
  ],
  PROVIDER_DEVICE_AUTHORIZATION_STARTED: [
    "Начата авторизация устройства",
    "Device authorization started",
  ],
  RESULT_ARTIFACT_AVAILABLE: [
    "Файл результата доступен",
    "Result file available",
  ],
  ROLE_IMAGE_BUILD_COMPLETED: [
    "Сборка образа роли завершена",
    "Role image build completed",
  ],
  ROLE_IMAGE_PROMOTED: [
    "Образ роли допущен к использованию",
    "Role image promoted",
  ],
  ROLE_IMAGE_PROMOTION_REQUESTED: [
    "Запрошен допуск образа роли",
    "Role image promotion requested",
  ],
  ROLE_IMAGE_RECIPE_CHANGED: [
    "Рецепт образа роли изменён",
    "Role image recipe changed",
  ],
  ROOT_PROCESS_COMPLETED: [
    "Основной Процесс завершён",
    "Root workflow completed",
  ],
  RUNTIME_ENVIRONMENT_CREATED: ["Окружение создано", "Environment created"],
  RUNTIME_ENVIRONMENT_DELETED: ["Окружение удалено", "Environment deleted"],
  RUNTIME_ENVIRONMENT_DISABLED: ["Окружение отключено", "Environment disabled"],
  RUNTIME_ENVIRONMENT_DRAFT_CHANGED: [
    "Черновик окружения изменён",
    "Environment draft changed",
  ],
  RUNTIME_ENVIRONMENT_ENABLED: ["Окружение включено", "Environment enabled"],
  RUNTIME_ENVIRONMENT_PUBLISHED: [
    "Ревизия окружения опубликована",
    "Environment revision published",
  ],
  RUNTIME_ENVIRONMENT_SELECTED_REBOUND: [
    "Выбранные привязки окружения обновлены",
    "Selected environment bindings updated",
  ],
  RUNTIME_EXECUTION_COMPLETED: [
    "Исполнение завершено",
    "Runtime execution completed",
  ],
  RUNTIME_LEASE_RENEWED: ["Срок исполнения продлён", "Runtime lease renewed"],
  RUNTIME_PROGRESS_RECORDED: [
    "Ход выполнения сохранён",
    "Runtime progress recorded",
  ],
  RUNTIME_SECRET_CREATED: ["Секрет создан", "Secret created"],
  RUNTIME_SECRET_DRAFT_CHANGED: [
    "Черновик секрета изменён",
    "Secret draft changed",
  ],
  RUNTIME_SECRET_OPERATION_FAILED: [
    "Операция с секретом завершилась ошибкой",
    "Secret operation failed",
  ],
  RUNTIME_SECRET_REVEALED: [
    "Значение секрета просмотрено",
    "Secret value viewed",
  ],
  RUNTIME_SECRET_REVOKED: ["Секрет отозван", "Secret revoked"],
  RUNTIME_SECRET_ROTATED: ["Секрет обновлён", "Secret rotated"],
  RUNTIME_SECRET_SELECTED_REBOUND: [
    "Выбранные привязки секрета обновлены",
    "Selected secret bindings updated",
  ],
  RUNTIME_TOOL_CALL_RECORDED: [
    "Вызов инструмента сохранён",
    "Tool call recorded",
  ],
  RUNTIME_WORK_CLAIMS_MATERIALIZED: [
    "Задания исполнения подготовлены",
    "Runtime work claims materialized",
  ],
  RUN_CANCELLED: ["Запуск отменён", "Run cancelled"],
  RUN_CANCELLED_BY_OWNER: [
    "Запуск отменён владельцем",
    "Run cancelled by owner",
  ],
  RUN_COMPLETED: ["Запуск завершён", "Run completed"],
  RUN_COORDINATION_ROLE: ["Координатор запуска", "Run coordinator"],
  RUN_CREATED: ["Запуск создан", "Run created"],
  RUN_METADATA_UPDATED: ["Сведения о запуске обновлены", "Run details updated"],
  RUN_NODE_CANCELLED: ["Этап запуска отменён", "Run node cancelled"],
  RUN_RETRY_CREATED: ["Повтор запуска создан", "Run retry created"],
  RUN_TURN_STARTED: ["Новый шаг сессии запущен", "New session turn started"],
  SCHEDULE_ARCHIVED: ["Автоматизация архивирована", "Automation archived"],
  SCHEDULE_CREATED: ["Автоматизация создана", "Automation created"],
  SCHEDULE_DELETED: ["Автоматизация удалена", "Automation deleted"],
  SCHEDULE_OCCURRENCE_FAILED: [
    "Запуск по расписанию завершился ошибкой",
    "Scheduled occurrence failed",
  ],
  SCHEDULE_OCCURRENCE_MATERIALIZED: [
    "Запуск по расписанию подготовлен",
    "Scheduled occurrence materialized",
  ],
  SCHEDULE_UPDATED: ["Автоматизация обновлена", "Automation updated"],
  SESSION_ARCHIVE_TASK_DEAD_LETTER: [
    "Не удалось завершить архивирование сессии",
    "Session archival could not be completed",
  ],
  SESSION_ARCHIVE_TASK_READY: [
    "Архивирование сессии подготовлено",
    "Session archival ready",
  ],
  SESSION_ARCHIVE_TASK_SUCCEEDED: [
    "Архивирование сессии завершено",
    "Session archival completed",
  ],
  SESSION_CONTINUATION: ["Продолжение сессии", "Session continuation"],
  SESSION_CONTINUED: ["Сессия продолжена", "Session continued"],
  SKILL_BUNDLE_CHANGED: ["Пакет навыка изменён", "Skill bundle changed"],
  SYSTEM_ASSISTANT_COMMAND: [
    "Команда системного помощника",
    "System assistant command",
  ],
  SYSTEM_ASSISTANT_CORE_PROMPT_UPDATED: [
    "Основные инструкции помощника обновлены",
    "Assistant core instructions updated",
  ],
  SYSTEM_ASSISTANT_HEARTBEAT_RECORDED: [
    "Состояние системного помощника подтверждено",
    "System assistant heartbeat recorded",
  ],
  SYSTEM_ASSISTANT_LABEL: ["Системный помощник", "System assistant"],
  SYSTEM_ASSISTANT_NAME: ["Kodex", "Kodex"],
  SYSTEM_ASSISTANT_PROVIDER_POLICY_UPDATED: [
    "Политика провайдеров помощника обновлена",
    "Assistant provider policy updated",
  ],
  SYSTEM_ASSISTANT_PURPOSE: [
    "Помощь в управлении платформой",
    "Help manage the platform",
  ],
  SYSTEM_ASSISTANT_ROLE_DESCRIPTION: [
    "Системный помощник владельца платформы",
    "Platform owner's system assistant",
  ],
  SYSTEM_ASSISTANT_RUNTIME_ENVIRONMENT_UPDATED: [
    "Окружение системного помощника обновлено",
    "System assistant environment updated",
  ],
  SYSTEM_BASE_ROLE_IMAGE: ["Базовый системный образ", "Base system role image"],
  SYSTEM_ROLE_ADMINISTRATOR: ["Администратор", "Administrator"],
  SYSTEM_ROLE_AUDITOR: ["Аудитор", "Auditor"],
  SYSTEM_ROLE_BOOTSTRAP: [
    "Начальная настройка системной роли",
    "System role initial setup",
  ],
  SYSTEM_ROLE_MEMBER: ["Участник", "Member"],
  SYSTEM_ROLE_OPERATOR: ["Оператор", "Operator"],
  SYSTEM_ROLE_OWNER: ["Владелец", "Owner"],
  WEB_ONLY_CORE_SUMMARY: [
    "Управление платформой через веб-интерфейс",
    "Manage the platform through the web interface",
  ],
  WORKFLOW_CREATED: ["Процесс создан", "Workflow created"],
  WORKFLOW_UPDATED: ["Процесс обновлён", "Workflow updated"],
} as const satisfies Record<string, readonly [string, string]>;

export const serverMessageTokens = new Set([
  ...Object.keys(serverTokenTranslations),
  ...Object.keys(serverPermissionTokens),
]);

export function serverMessagesFor(
  locale: "ru" | "en",
  permissions: Record<ServerPermissionKey, PermissionLabel>,
): Record<string, string> {
  const column = locale === "ru" ? 0 : 1;
  return Object.fromEntries<string>([
    ...Object.entries(serverTokenTranslations).map(
      ([key, messages]): [string, string] => [key, messages[column]],
    ),
    ...Object.entries(serverPermissionTokens).map(
      ([key, [permission, field]]): [string, string] => [
        key,
        permissions[permission][field],
      ],
    ),
  ]);
}
