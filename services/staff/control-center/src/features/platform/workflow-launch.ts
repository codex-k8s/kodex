import type { Workflow } from "@/shared/api/generated/openapi/types.gen";

export function workflowLaunchReadiness(workflow: Workflow | undefined) {
  const readiness = workflow?.launchReadiness;
  if (
    !workflow ||
    !readiness ||
    readiness.workflowVersion !== workflow.version ||
    (readiness.revisionRef ?? "") !== (workflow.publishedRevisionRef ?? "") ||
    !/^[a-f0-9]{64}$/.test(readiness.contextDigest) ||
    (workflow.state === "DRAFT" && readiness.allowedToSubmit)
  )
    return undefined;
  return readiness;
}

export const workflowLaunchMessages = {
  ru: {
    missing: "Серверная проверка запуска недоступна. Обновите процесс.",
    reasons: {
      READY: "Запуск разрешён",
      PERMISSION_REQUIRED: "У вас нет права запускать этот процесс.",
      UNPUBLISHED: "Опубликуйте изменения процесса перед запуском.",
      DEPENDENCY_UNAVAILABLE:
        "Зависимости процесса недоступны. Проверьте сотрудников и их конфигурацию.",
    },
    operational: {
      READY: "Готовность выполнения подтверждена.",
      BLOCKED:
        "Отправка разрешена; выполнение может ожидать готовности или свободной ёмкости.",
      UNKNOWN: "Отправка разрешена; состояние выполнения ещё не подтверждено.",
    },
  },
  en: {
    missing: "Server launch verification is unavailable. Refresh the process.",
    reasons: {
      READY: "Launch is allowed",
      PERMISSION_REQUIRED: "You do not have permission to launch this process.",
      UNPUBLISHED: "Publish the process changes before launching.",
      DEPENDENCY_UNAVAILABLE:
        "Process dependencies are unavailable. Check its agents and their configuration.",
    },
    operational: {
      READY: "Execution readiness is confirmed.",
      BLOCKED:
        "Submission is allowed; execution may wait for readiness or available capacity.",
      UNKNOWN: "Submission is allowed; execution status is not yet confirmed.",
    },
  },
};
