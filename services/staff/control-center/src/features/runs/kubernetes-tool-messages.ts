export const kubernetesToolMessages = {
  ru: {
    title: "Инструменты Kubernetes",
    description:
      "Остановите отдельный вызов, в том числе пока он ждёт согласования. Запуск целиком не отменяется.",
    refresh: "Обновить",
    next: "Следующие",
    empty: "Вызовов Kubernetes в этом узле пока нет.",
    stop: "Остановить вызов",
    waiting: "Ждёт согласования",
    portForwardProfileUnsupported:
      "Вызов отклонён до открытия потока: полный port-forward требует закреплённого оператором профиля PORT_FORWARD_SPDY_V1. Профиль WebSocket v4 не поддерживает этот путь.",
    uncertain:
      "Остановка пока не подтверждена. Проверяем сохранённое состояние.",
    details: "Область вызова",
    closure:
      "Закрытие вызова не подтверждает остановку уже запущенного удалённого процесса.",
  },
  en: {
    title: "Kubernetes tools",
    description:
      "Stop one call, including while it awaits approval. The whole run is not cancelled.",
    refresh: "Refresh",
    next: "Next",
    empty: "This node has no Kubernetes calls yet.",
    stop: "Stop call",
    waiting: "Awaiting approval",
    portForwardProfileUnsupported:
      "The call was rejected before opening a stream: full port-forward requires an operator-pinned PORT_FORWARD_SPDY_V1 profile. WebSocket v4 does not support this path.",
    uncertain: "Stop is not confirmed yet. Checking the saved state.",
    details: "Call scope",
    closure:
      "Closing the call does not confirm termination of an already running remote process.",
  },
};
