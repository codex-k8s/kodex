import type { RunEvent } from "@/shared/api/generated/openapi/types.gen";

const opaqueRefPattern =
  /`?(?:agt|art|bld|cap|cnv|con|edg|evt|gat|inc|int|job|mbr|msg|nod|pln|prj|rev|rol|rti|run|sch|ses|trn|usr|wfl)_[A-Za-z0-9_-]{8,}`?/g;
const technicalTokenPattern = /`?\b[A-Z][A-Z\d]*(?:_[A-Z\d]+)+\b`?/g;
const conversationalMessageKinds = new Set<RunEvent["messageKind"]>([
  "USER_MESSAGE",
  "ASSISTANT_MESSAGE",
  "INTERMEDIATE_MESSAGE",
  "FINAL_MESSAGE",
]);

export function runtimeProgressKey(
  value: string | undefined,
): string | undefined {
  switch (value?.trim()) {
    case "WORKLOAD_SCHEDULED":
      return "runs.runtimeProgress.workloadScheduled";
    case "MODEL_REQUEST_RUNNING":
      return "runs.runtimeProgress.modelRequestRunning";
    default:
      return undefined;
  }
}

export function presentRuntimeText(
  value: string | undefined,
  serverMessage: (value: string) => string,
  messageKind?: RunEvent["messageKind"],
): string | undefined {
  const source = value?.trim();
  if (!source) return undefined;
  const conversational = conversationalMessageKinds.has(messageKind);
  if (!conversational && (source.startsWith("{") || source.startsWith("["))) {
    try {
      JSON.parse(source);
      return undefined;
    } catch {
      // Пользовательское предложение может начинаться со скобки.
    }
  }
  let visible = serverMessage(source)
    .replace(/`?i18n:[A-Z\d_]+`?/g, "")
    .replace(opaqueRefPattern, "");
  if (conversational) {
    visible = visible.replace(technicalTokenPattern, (token) =>
      token.startsWith("`") && token.endsWith("`") ? token : `\`${token}\``,
    );
  } else {
    visible = visible.replace(technicalTokenPattern, "");
  }
  visible = visible
    .replace(/\s+([,.;:!?])/g, "$1")
    .replace(/([,.;:])\s*([,.;:])/g, "$1")
    .replace(/^\s*[-–—:;,]+\s*|\s*[-–—:;,]+\s*$/g, "")
    .replace(/\s{2,}/g, " ")
    .trim();
  return /[\p{L}\p{N}]/u.test(visible) ? visible : undefined;
}
