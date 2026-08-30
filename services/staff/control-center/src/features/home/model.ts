import type { OwnerGate, Run } from "@/shared/api/generated/openapi/types.gen";

export type HomeAttentionCategory = "HUMAN_GATE" | "RUN_FAILURE";

function runActivityAt(run: Run): string {
  return run.finishedAt ?? run.startedAt ?? run.createdAt;
}

function isStoppedByFailure(run: Run): boolean {
  if (run.state === "FAILED") return true;
  return (
    run.state === "CANCELLED" &&
    run.nextActions.includes("RETRY") &&
    Boolean(run.safeErrorCode || run.safeErrorMessage)
  );
}

export function homeOpenGates(gates: OwnerGate[]): OwnerGate[] {
  return gates
    .filter((gate) => gate.state === "OPEN")
    .sort((left, right) => {
      const leftDeadline = left.expiresAt ?? "9999-12-31T23:59:59Z";
      const rightDeadline = right.expiresAt ?? "9999-12-31T23:59:59Z";
      return (
        leftDeadline.localeCompare(rightDeadline) ||
        right.openedAt.localeCompare(left.openedAt)
      );
    });
}

export function homeFailedRuns(runs: Run[], limit = 8): Run[] {
  return runs
    .filter(isStoppedByFailure)
    .sort((left, right) =>
      runActivityAt(right).localeCompare(runActivityAt(left)),
    )
    .slice(0, limit);
}

export function homeResumableSessions(runs: Run[], limit = 6): Run[] {
  const failed = new Set(
    homeFailedRuns(runs, runs.length).map((run) => run.ref),
  );
  const seenSessions = new Set<string>();

  return [...runs]
    .filter(
      (run) =>
        !failed.has(run.ref) &&
        !["QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"].includes(
          run.state,
        ) &&
        run.nextActions.includes("ADD_TURN"),
    )
    .sort((left, right) =>
      runActivityAt(right).localeCompare(runActivityAt(left)),
    )
    .filter((run) => {
      if (seenSessions.has(run.sessionRef)) return false;
      seenSessions.add(run.sessionRef);
      return true;
    })
    .slice(0, limit);
}
