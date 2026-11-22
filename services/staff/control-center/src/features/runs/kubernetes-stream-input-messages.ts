export const kubernetesStreamInputMessages = {
  ru: {
    change: "Выбрать другой поток",
    title: "Ввод и остановка Kubernetes-инструмента",
    description:
      "Выберите текущий поток этой сессии. Доступ и каждую передачу проверяет сервер.",
    empty: "Доступных потоков пока нет.",
    refresh: "Обновить",
    next: "Следующая страница",
    choose: "Выбрать поток",
    selected: "Выбранный поток",
    target: "Цель",
    scope: "Область",
    source: "Ресурс",
    state: "Состояние",
    input: "Стандартный ввод",
    send: "Отправить",
    eof: "Закрыть стандартный ввод",
    resize: "Размер терминала",
    columns: "Столбцы",
    rows: "Строки",
    applySize: "Изменить размер",
    stop: "Остановить этот инструмент",
    stopNote:
      "Остановка закрывает передачу на сервере. Завершение удалённого процесса подтверждается отдельно.",
    stagingQuota:
      "Лимит временных данных: {bytes} байт и {objects} сообщений одновременно для организации. Подтверждённая очистка освобождает квоту.",
    deliveryNote:
      "Подтверждение ввода означает локальную передачу в транспорт, а не успешное выполнение команды.",
    uncertain:
      "Исход передачи не подтверждён. Байты повторно не отправляются; обновите состояние или остановите инструмент.",
    privateSource:
      "Файл передаётся только из закреплённого сервером артефакта. Загрузка произвольного файла из этой панели недоступна.",
    tunnel:
      "Перенаправление порта использует защищённый локальный мост. Эта панель не открывает TCP-соединение в браузере.",
    readonly: "Этот поток не принимает пользовательский ввод.",
    pending: "Передача",
    closed: "Передача ввода закрыта.",
    tooLarge: "Ввод превышает допустимый размер одного сообщения.",
    states: {
      PREPARED: "Подготовлен",
      ACTIVE: "Активен",
      COMPLETED: "Завершён",
      CANCELED: "Закрыт владельцем",
      REVOKED: "Доступ отозван",
      EXPIRED: "Срок истёк",
      LOST: "Связь потеряна",
    },
    submissions: {
      QUEUED: "Ожидает передачи",
      CLAIMED: "Передаётся",
      ACKNOWLEDGED: "Локальная передача подтверждена",
      UNKNOWN: "Исход неизвестен",
      REJECTED: "Отклонена",
      CANCELLED: "Закрыта",
    },
  },
  en: {
    change: "Select another stream",
    title: "Kubernetes tool input and stop",
    description:
      "Select a current stream for this session. The server checks access and every delivery.",
    empty: "No streams are available yet.",
    refresh: "Refresh",
    next: "Next page",
    choose: "Select stream",
    selected: "Selected stream",
    target: "Target",
    scope: "Scope",
    source: "Resource",
    state: "State",
    input: "Standard input",
    send: "Send",
    eof: "Close standard input",
    resize: "Terminal size",
    columns: "Columns",
    rows: "Rows",
    applySize: "Resize",
    stop: "Stop this tool",
    stopNote:
      "Stop closes server-side delivery. Remote process termination requires separate confirmation.",
    stagingQuota:
      "Temporary storage limit: {bytes} bytes and {objects} messages at once for the organization. Confirmed cleanup releases quota.",
    deliveryNote:
      "Input acknowledgement confirms a local transport write, not successful command execution.",
    uncertain:
      "Delivery is unconfirmed. Bytes are never resent; refresh metadata or stop the tool.",
    privateSource:
      "Files are delivered only from a server-pinned artifact. Arbitrary file upload is unavailable in this panel.",
    tunnel:
      "Port forwarding requires a protected local bridge. This panel does not open a browser TCP connection.",
    readonly: "This stream does not accept user input.",
    pending: "Delivery",
    closed: "Input delivery is closed.",
    tooLarge: "Input exceeds the maximum message size.",
    states: {
      PREPARED: "Prepared",
      ACTIVE: "Active",
      COMPLETED: "Completed",
      CANCELED: "Closed by owner",
      REVOKED: "Access revoked",
      EXPIRED: "Expired",
      LOST: "Connection lost",
    },
    submissions: {
      QUEUED: "Pending delivery",
      CLAIMED: "Delivering",
      ACKNOWLEDGED: "Local write acknowledged",
      UNKNOWN: "Outcome unknown",
      REJECTED: "Rejected",
      CANCELLED: "Closed",
    },
  },
};
