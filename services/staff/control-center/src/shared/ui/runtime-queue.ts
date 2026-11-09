import type { Run } from "@/shared/api/generated/openapi/types.gen";

// Только server-owned queueReason управляет сообщением, текст модели не меняет статус.
export function runtimeQueueMessage(
  run?: Pick<Run, "state" | "queueReason">,
): string | undefined {
  if (!run || !["QUEUED", "RUNNING"].includes(run.state)) return undefined;
  switch (run.queueReason) {
    case "WAITING_RESOURCES":
      return "i18n:RUNTIME_WAITING_CAPACITY";
    case "REQUEST_EXCEEDS_CAPACITY":
      return "i18n:RUNTIME_REQUEST_EXCEEDS_CAPACITY";
    default:
      return undefined;
  }
}
