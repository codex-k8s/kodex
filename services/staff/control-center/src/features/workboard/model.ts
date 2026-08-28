import type {
  Artifact,
  OwnerGate,
  Run,
} from "@/shared/api/generated/openapi/types.gen";

export type RunFilter = "ALL" | "ACTIVE" | "TERMINAL";
export type RunView = "KANBAN" | "LIST";
export type RunLane = "QUEUED" | "RUNNING" | "WAITING_HUMAN" | "TERMINAL";

export type AttentionItem =
  | { kind: "GATE"; ref: string; run: Run | undefined; gate: OwnerGate }
  | {
      kind: "INCIDENT";
      ref: string;
      run: Run;
      incident: NonNullable<Run["incidents"]>[number];
    };

const activeStates = new Set<Run["state"]>([
  "QUEUED",
  "RUNNING",
  "WAITING_HUMAN",
  "CANCELLING",
]);

export function isActiveRun(run: Run): boolean {
  return activeStates.has(run.state);
}

export function isTerminalRun(run: Run): boolean {
  return !isActiveRun(run);
}

export function runLane(run: Run): RunLane {
  if (run.state === "QUEUED") return "QUEUED";
  if (run.state === "WAITING_HUMAN") return "WAITING_HUMAN";
  if (run.state === "RUNNING" || run.state === "CANCELLING") return "RUNNING";
  return "TERMINAL";
}

export function filterRuns(runs: Run[], filter: RunFilter): Run[] {
  return [...runs]
    .filter((run) => {
      if (filter === "ACTIVE") return isActiveRun(run);
      if (filter === "TERMINAL") return isTerminalRun(run);
      return true;
    })
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt));
}

export function groupRuns(runs: Run[]): Record<RunLane, Run[]> {
  const lanes: Record<RunLane, Run[]> = {
    QUEUED: [],
    RUNNING: [],
    WAITING_HUMAN: [],
    TERMINAL: [],
  };
  for (const run of runs) lanes[runLane(run)].push(run);
  return lanes;
}

export function runExecutor(run: Run): string | undefined {
  return run.target.type === "AGENT" ? run.target.displayName : undefined;
}

export function collectAttention(
  runs: Run[],
  gates: OwnerGate[],
): AttentionItem[] {
  const byRef = new Map(runs.map((run) => [run.ref, run]));
  const items: AttentionItem[] = [];

  for (const gate of gates) {
    if (gate.state !== "OPEN") continue;
    items.push({
      kind: "GATE",
      ref: gate.ref,
      run: byRef.get(gate.runRef),
      gate,
    });
  }
  for (const run of runs) {
    for (const incident of run.incidents ?? []) {
      if (incident.state === "RESOLVED") continue;
      items.push({ kind: "INCIDENT", ref: incident.ref, run, incident });
    }
  }

  return items.sort((left, right) => {
    const leftAt =
      left.kind === "GATE" ? left.gate.openedAt : left.incident.createdAt;
    const rightAt =
      right.kind === "GATE" ? right.gate.openedAt : right.incident.createdAt;
    return rightAt.localeCompare(leftAt);
  });
}

export function projectArtifacts(
  artifacts: Artifact[],
  projectRef?: string,
): Artifact[] {
  return artifacts
    .filter((artifact) => !projectRef || artifact.projectRef === projectRef)
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt));
}
