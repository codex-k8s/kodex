import type {
  OwnerGate,
  Project,
  Run,
} from "@/shared/api/generated/openapi/types.gen";

export type HomeAttentionCategory = "HUMAN_GATE" | "RUN_FAILURE";

export function prioritizeHomeProjects(
  projects: readonly Project[],
  limit = 6,
): Project[] {
  return [...projects]
    .sort(
      (left, right) =>
        right.pendingGateCount - left.pendingGateCount ||
        right.activeRunCount - left.activeRunCount ||
        right.updatedAt.localeCompare(left.updatedAt) ||
        left.name.localeCompare(right.name),
    )
    .slice(0, Math.max(0, limit));
}

export function homePriorityProjectRefs(
  gates: readonly OwnerGate[],
  activeRuns: readonly Run[],
  failedRuns: readonly Run[],
  limit = 12,
): string[] {
  const projects = new Map<
    string,
    { gates: number; runs: number; failures: number }
  >();
  const count = (ref: string, kind: "gates" | "runs" | "failures") => {
    if (!ref) return;
    const current = projects.get(ref) ?? { gates: 0, runs: 0, failures: 0 };
    current[kind] += 1;
    projects.set(ref, current);
  };
  for (const gate of gates)
    if (gate.state === "OPEN") count(gate.projectRef, "gates");
  for (const run of activeRuns) count(run.projectRef, "runs");
  for (const run of failedRuns) count(run.projectRef, "failures");
  return [...projects]
    .sort(
      ([leftRef, left], [rightRef, right]) =>
        right.gates - left.gates ||
        right.runs - left.runs ||
        right.failures - left.failures ||
        leftRef.localeCompare(rightRef),
    )
    .slice(0, Math.max(0, limit))
    .map(([ref]) => ref);
}

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
