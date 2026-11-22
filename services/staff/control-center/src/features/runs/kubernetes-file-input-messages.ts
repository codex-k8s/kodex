export const kubernetesFileMessages = {
  ru: {
    title: "Источник копирования",
    description:
      "Владелец берёт файл из каталога исходного хода. Ключи Kubernetes и содержимое файла не передаются в этот интерфейс.",
    refresh: "Обновить источник",
    empty: "Подготовленный источник пока не обнаружен.",
    progress:
      "INPUT ACK: {acknowledged} из {total} байт; подготовлено к передаче: {staged}.",
    eof: "Все байты и EOF приняты INPUT transport. Это не подтверждает удалённое выполнение.",
    integrity: "Закреплённая ревизия и дайджесты",
    limit:
      "Новый файл доступен после штатной загрузки и нового хода с новым каталогом. READY относится к материалу; результат копирования определяется отдельной квитанцией инструмента.",
    cleanupTitle: "Очистка технического источника",
    cleanupStatus:
      "Осталось объектов: {count}, байт: {bytes}; попытки исчерпаны: {exhausted}.",
    cleanupRepair: "Возобновить очистку",
    cleanupRepairUnavailable: "Ручное возобновление очистки пока недоступно.",
    cleanupRefresh: "Проверить очистку",
    cleanupAccepted:
      "Новый цикл очистки принят. Это ещё не подтверждает удаление объекта.",
    cleanupUncertain:
      "Ответ на запрос неизвестен. Разрешена только сверка исходного запроса; повторной записи нет.",
    cleanupLimit:
      "Нужны права просмотра исходного запуска и загрузки артефактов в его проект. Доступ к содержимому исходного файла не требуется. Очистка не повторяет копирование в Kubernetes.",
    states: {
      PREPARING: "Подготовка",
      READY: "Материал подготовлен",
      UNKNOWN: "Исход неизвестен",
      REJECTED: "Источник отклонён",
      CANCELLED: "Источник закрыт",
      TRANSFERRED: "INPUT подтверждён",
      EXPIRED: "Срок истёк",
    },
    reasons: {
      SOURCE_UNAVAILABLE:
        "Точный источник недоступен; автоматической повторной записи нет.",
      SOURCE_CHANGED: "Закреплённая ревизия или квитанция не совпали.",
      PERMISSION_LOST: "Текущих прав недостаточно для новых байтов INPUT.",
      CONTENT_REJECTED: "Источник не прошёл проверку формата или антивируса.",
      INPUT_UNKNOWN:
        "Исход передачи INPUT неизвестен. Те же байты автоматически не отправляются повторно.",
      CANCELLED: "Исходный вызов или поток закрыт.",
      EXPIRED: "Исходный срок допуска истёк.",
    },
  },
  en: {
    title: "Copy source",
    description:
      "The owner selects the file from the original turn's catalog. Kubernetes credentials and file content are not sent to this interface.",
    refresh: "Refresh source",
    empty: "No prepared source has been discovered yet.",
    progress: "INPUT ACK: {acknowledged} of {total} bytes; staged: {staged}.",
    eof: "All bytes and EOF were accepted by the INPUT transport. This does not confirm remote execution.",
    integrity: "Pinned revision and digests",
    limit:
      "A new file becomes available after the existing upload flow and a new turn with a new catalog. READY describes material; a separate tool receipt determines the copy outcome.",
    cleanupTitle: "Technical source cleanup",
    cleanupStatus:
      "Outstanding objects: {count}, bytes: {bytes}; exhausted: {exhausted}.",
    cleanupRepair: "Resume cleanup",
    cleanupRepairUnavailable:
      "Manual cleanup recovery is currently unavailable.",
    cleanupRefresh: "Check cleanup",
    cleanupAccepted:
      "A new cleanup cycle was accepted. Object deletion is not confirmed yet.",
    cleanupUncertain:
      "The request outcome is unknown. Only the original request can be read back; no write is repeated.",
    cleanupLimit:
      "Viewing the original run and uploading artifacts in its project are required. Source content access is not required. Cleanup never repeats the Kubernetes copy.",
    states: {
      PREPARING: "Preparing",
      READY: "Material prepared",
      UNKNOWN: "Unknown outcome",
      REJECTED: "Source rejected",
      CANCELLED: "Source closed",
      TRANSFERRED: "INPUT acknowledged",
      EXPIRED: "Expired",
    },
    reasons: {
      SOURCE_UNAVAILABLE:
        "The exact source is unavailable; no automatic write replay occurs.",
      SOURCE_CHANGED: "The pinned revision or receipt did not match.",
      PERMISSION_LOST: "Current permissions do not allow new INPUT bytes.",
      CONTENT_REJECTED: "The source failed format or antivirus checks.",
      INPUT_UNKNOWN:
        "INPUT delivery is uncertain. The same bytes are not automatically resent.",
      CANCELLED: "The original invocation or stream is closed.",
      EXPIRED: "The original allowance expired.",
    },
  },
};
