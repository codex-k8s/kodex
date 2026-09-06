import { AppProblem } from "./problem";

export class KnownMutationRejection extends AppProblem {
  constructor(status: 400 | 412 | 422) {
    super({
      status,
      code: status === 412 ? "VERSION_OR_STATE_CONFLICT" : "INVALID_REQUEST",
      retryable: false,
      kind: status === 412 ? "conflict" : "unknown",
    });
  }
}

// Только для команд, где эти отказы предшествуют эффекту, и одной transport
// attempt. Даже такой ответ на повтор не разрешает забыть прежний UNKNOWN.
export function checkMutationRejection<
  T extends {
    error?: unknown;
    response?: Response;
  },
>(result: T): T {
  const { error, response } = result;
  if (
    !response ||
    response.headers
      .get("Content-Type")
      ?.split(";", 1)[0]
      ?.trim()
      .toLowerCase() !== "application/problem+json" ||
    typeof error !== "object" ||
    error === null ||
    !("status" in error) ||
    error.status !== response.status ||
    !("code" in error) ||
    !("retryable" in error)
  )
    return result;
  const status = response.status;
  if (
    ((status === 400 || status === 422) &&
      error.code === "INVALID_REQUEST" &&
      error.retryable === false) ||
    (status === 412 &&
      error.code === "VERSION_OR_STATE_CONFLICT" &&
      error.retryable === true)
  )
    throw new KnownMutationRejection(status);
  return result;
}
