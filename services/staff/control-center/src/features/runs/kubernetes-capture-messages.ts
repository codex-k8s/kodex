export const kubernetesCaptureMessages = {
  ru: {
    historyTitle: "Сохранённая история Kubernetes",
    historyRefresh: "Обновить историю",
    historyNext: "Следующая страница истории",
    historyEmpty: "Сохранённых результатов пока нет.",
    cleanupStatus:
      "Ожидают удаления: {count}, {bytes} байт. Исчерпан цикл очистки: {exhausted}.",
    cleanupRepair: "Возобновить очистку временных данных",
    cleanupExplanation:
      "Требуется текущее право загрузки артефактов. Восстановление сохраняет историю попыток; место освободится только после подтверждённого удаления. Команда не запускается повторно.",
    cleanupReceipt:
      "Восстановлено записей: {count}. Ещё требуют отдельного восстановления: {remaining}.",
    title: "Сохранение результата",
    explanation:
      "Сохранение принятой истории отдельно от исполнения. Восстановление не запускает команду повторно и требует текущего права загрузки артефактов.",
    refresh: "Обновить сохранение",
    repair: "Возобновить сохранение",
    permission:
      "Доступно исходному пользователю с правом загрузки артефактов. Удалённое исполнение не возобновится.",
    uncertain:
      "Квитанция восстановления пока не подтверждена. Повторный запрос не отправлен.",
    attempts: "Всего попыток: {count}",
    partial:
      "Сохранён подтверждённый префикс. Полнота вывода и успешное завершение команды не подтверждены.",
    bytes: "байт",
    reason: {
      CAPTURE_ELIGIBILITY_CHANGED:
        "Текущий доступ к исходной истории изменился.",
      CAPTURE_SOURCE_INVALID: "Не подтверждён исходный снимок истории.",
      CAPTURE_STORAGE_UNKNOWN: "Результат записи в хранилище пока неизвестен.",
      CAPTURE_RETRY_EXHAUSTED:
        "Исчерпан ограниченный цикл технических попыток.",
      CAPTURE_RETENTION_EXPIRED: "Срок хранения исходных данных истёк.",
      CAPTURE_CONTENT_REJECTED: "Содержимое не прошло проверку артефакта.",
      CAPTURE_SUPERSEDED:
        "Поздний подтверждённый вывод сохранён следующим поколением истории.",
    },
  },
  en: {
    historyTitle: "Retained Kubernetes history",
    historyRefresh: "Refresh history",
    historyNext: "Next history page",
    historyEmpty: "No retained results yet.",
    cleanupStatus:
      "Pending deletion: {count}, {bytes} bytes. Exhausted cleanup cycles: {exhausted}.",
    cleanupRepair: "Resume temporary data cleanup",
    cleanupExplanation:
      "Current artifact upload permission is required. Attempt history is preserved; storage is released only after confirmed deletion. The command will not execute again.",
    cleanupReceipt:
      "Repaired reservations: {count}. Still requiring explicit repair: {remaining}.",
    title: "Result capture",
    explanation:
      "Accepted history is retained separately from execution. Repair does not execute the command again and requires current artifact upload permission.",
    refresh: "Refresh capture",
    repair: "Resume capture",
    permission:
      "Available to the original user with artifact upload permission. Remote execution will not resume.",
    uncertain:
      "The repair receipt is not confirmed. No repeated request was sent.",
    attempts: "Total attempts: {count}",
    partial:
      "A confirmed prefix was retained. Complete output and command success are not confirmed.",
    bytes: "bytes",
    reason: {
      CAPTURE_ELIGIBILITY_CHANGED:
        "Current access to the original history changed.",
      CAPTURE_SOURCE_INVALID:
        "The original history snapshot could not be confirmed.",
      CAPTURE_STORAGE_UNKNOWN: "The storage write outcome is not confirmed.",
      CAPTURE_RETRY_EXHAUSTED:
        "The bounded technical attempt cycle is exhausted.",
      CAPTURE_RETENTION_EXPIRED: "Source retention expired.",
      CAPTURE_CONTENT_REJECTED: "Content did not pass artifact inspection.",
      CAPTURE_SUPERSEDED:
        "Late confirmed output is retained by a successor capture generation.",
    },
  },
};
