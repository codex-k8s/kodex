import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { serverMessageKey } from "@/shared/ui/server-message";

export function runListSummary(run: Run): string | undefined {
  if (run.state !== "FAILED") return run.currentActivity ?? run.resultSummary;

  if (run.safeErrorCode) {
    const localizedCode = `i18n:${run.safeErrorCode}`;
    if (serverMessageKey(localizedCode)) return localizedCode;
  }
  const message = run.safeErrorMessage ?? run.resultSummary;
  return message && !/^[A-Z][A-Z0-9_]*$/.test(message) ? message : undefined;
}
