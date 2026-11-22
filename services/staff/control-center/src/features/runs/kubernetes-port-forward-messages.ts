export const kubernetesPortForwardMessages = {
  ru: {
    title: "TCP-соединения разрешённого туннеля",
    description:
      "Здесь управление отдельными соединениями и передача текстовых байтов. Браузер не открывает локальный TCP listener и не заменяет защищённый клиент вывода. Адрес и порты назначены исходным допуском.",
    refresh: "Обновить сведения",
    next: "Следующая страница",
    capacity:
      "Активно {active} из {max}; исходный timeout — {seconds} с. Последовательные соединения ограничены общими bytes и deadline туннеля.",
    mapping: "Разрешённая пара портов",
    chooseMapping: "Выберите пару портов",
    open: "Открыть соединение",
    slot: "Пара {slot}",
    uncertain:
      "Ответ потерян или исход неизвестен. Байты автоматически повторно не отправляются; доступны только сверка сведений и закрытие.",
    joined:
      "Получено подтверждение локального завершения native pair. Удалённый успех этим не подтверждён.",
    joinUnknown:
      "Завершение native pair не подтверждено. Закрытие владельцем не означает удалённый FIN или успешную доставку.",
    writeClosed:
      "Локальная передача закрыта; обратный поток может продолжаться.",
    readEnded:
      "Наблюдалось завершение чтения. FIN и RST не различаются; это не подтверждение удалённого успеха.",
    data: "Текстовые байты UTF-8 для выбранного соединения",
    send: "Передать байты",
    halfClose: "Закрыть направление записи",
    close: "Закрыть выбранное соединение",
    states: {
      PREPARED: "Ожидает допуска OPEN",
      OPENING: "OPEN допущен; подтверждения пока нет",
      OPEN: "Локальное открытие подтверждено",
      CLOSED: "Закрыто владельцем",
      UNKNOWN: "Исход неизвестен",
      CANCELLED: "Отменено",
      EXPIRED: "Срок истёк",
    },
    reasons: {
      KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED:
        "Выбранный транспорт не поддерживает полный жизненный цикл TCP-соединения. Требуется закреплённый оператором профиль SPDY; автоматического переключения нет.",
      KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED:
        "Достигнут лимит одновременно активных соединений.",
      KUBERNETES_RECONCILE_REQUIRED:
        "Нужна сверка текущего состояния исходного туннеля.",
    },
  },
  en: {
    title: "Approved tunnel TCP connections",
    description:
      "Control individual connections and send UTF-8 bytes. The browser does not open a local TCP listener or replace the protected output client. The original admission pins the destination and ports.",
    refresh: "Refresh metadata",
    next: "Next page",
    capacity:
      "Active {active} of {max}; original timeout {seconds}s. Sequential connections remain bounded by the tunnel bytes and deadline.",
    mapping: "Approved port mapping",
    chooseMapping: "Select a mapping",
    open: "Open connection",
    slot: "Mapping {slot}",
    uncertain:
      "The reply was lost or the outcome is unknown. Bytes are never resubmitted automatically; reconcile metadata or close.",
    joined:
      "Local native pair join is confirmed. This does not prove remote success.",
    joinUnknown:
      "Native pair join is unconfirmed. Owner closure does not prove remote FIN or delivery.",
    writeClosed: "Local writes are closed; reverse traffic may continue.",
    readEnded:
      "Read end was observed. FIN and RST are not distinguished and do not prove remote success.",
    data: "UTF-8 bytes for this connection",
    send: "Send bytes",
    halfClose: "Close write direction",
    close: "Close selected connection",
    states: {
      PREPARED: "Awaiting OPEN admission",
      OPENING: "OPEN admitted; acknowledgement pending",
      OPEN: "Local open acknowledged",
      CLOSED: "Owner closed",
      UNKNOWN: "Unknown outcome",
      CANCELLED: "Cancelled",
      EXPIRED: "Expired",
    },
    reasons: {
      KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED:
        "The selected transport does not support the full TCP connection lifecycle. An operator-pinned SPDY profile is required; no automatic fallback is used.",
      KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED:
        "The simultaneous connection limit is reached.",
      KUBERNETES_RECONCILE_REQUIRED: "Reconcile the original tunnel state.",
    },
  },
};
