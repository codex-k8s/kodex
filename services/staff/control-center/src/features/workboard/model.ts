import type {
  Artifact,
  OwnerGate,
  Project,
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

export type DecisionUrgency = "OVERDUE" | "SOON" | "NORMAL";

export interface DecisionInboxItem {
  gate: OwnerGate;
  project?: Project;
  run?: Run;
  urgency: DecisionUrgency;
  hasQuestion: boolean;
  hasConsequences: boolean;
  canResolve: boolean;
}

export interface DecisionInboxGroup {
  key: string;
  urgency: DecisionUrgency;
  project?: Project;
  run?: Run;
  items: DecisionInboxItem[];
}

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

export function decisionUrgency(
  gate: OwnerGate,
  now = new Date(),
): DecisionUrgency {
  if (!gate.expiresAt) return "NORMAL";
  const expiresAt = Date.parse(gate.expiresAt);
  if (!Number.isFinite(expiresAt)) return "NORMAL";
  if (expiresAt <= now.getTime()) return "OVERDUE";
  return expiresAt - now.getTime() <= 24 * 60 * 60 * 1000 ? "SOON" : "NORMAL";
}

export function decisionInbox(
  gates: OwnerGate[],
  projects: Project[],
  projectRef?: string,
  now = new Date(),
  runs: Run[] = [],
): DecisionInboxItem[] {
  const projectsByRef = new Map(
    projects.map((project) => [project.ref, project]),
  );
  const runsByRef = new Map(runs.map((run) => [run.ref, run]));
  const urgencyOrder: Record<DecisionUrgency, number> = {
    OVERDUE: 0,
    SOON: 1,
    NORMAL: 2,
  };

  return gates
    .filter(
      (gate) =>
        gate.state === "OPEN" &&
        (!projectRef || gate.projectRef === projectRef),
    )
    .map((gate) => {
      const hasQuestion = gate.contextSummary.trim().length > 0;
      const hasConsequences = gate.consequencesSummary.trim().length > 0;
      return {
        gate,
        project: projectsByRef.get(gate.projectRef),
        run: runsByRef.get(gate.runRef),
        urgency: decisionUrgency(gate, now),
        hasQuestion,
        hasConsequences,
        canResolve:
          hasQuestion &&
          hasConsequences &&
          gate.nextActions.includes("RESOLVE_GATE") &&
          gate.allowedDecisions.length > 0,
      };
    })
    .sort((left, right) => {
      const urgency = urgencyOrder[left.urgency] - urgencyOrder[right.urgency];
      if (urgency !== 0) return urgency;
      const leftDeadline = left.gate.expiresAt ?? "9999-12-31T23:59:59Z";
      const rightDeadline = right.gate.expiresAt ?? "9999-12-31T23:59:59Z";
      const deadline = leftDeadline.localeCompare(rightDeadline);
      return deadline || right.gate.openedAt.localeCompare(left.gate.openedAt);
    });
}

export function decisionHistory(
  gates: OwnerGate[],
  projects: Project[],
  projectRef?: string,
  runs: Run[] = [],
): DecisionInboxItem[] {
  const projectsByRef = new Map(
    projects.map((project) => [project.ref, project]),
  );
  const runsByRef = new Map(runs.map((run) => [run.ref, run]));
  return gates
    .filter(
      (gate) =>
        gate.state !== "OPEN" &&
        (!projectRef || gate.projectRef === projectRef),
    )
    .map((gate) => ({
      gate,
      project: projectsByRef.get(gate.projectRef),
      run: runsByRef.get(gate.runRef),
      urgency: "NORMAL" as const,
      hasQuestion: gate.contextSummary.trim().length > 0,
      hasConsequences: gate.consequencesSummary.trim().length > 0,
      canResolve: false,
    }))
    .sort((left, right) =>
      (right.gate.decidedAt ?? right.gate.openedAt).localeCompare(
        left.gate.decidedAt ?? left.gate.openedAt,
      ),
    );
}

export function groupDecisionInbox(
  items: DecisionInboxItem[],
): DecisionInboxGroup[] {
  const groups = new Map<string, DecisionInboxGroup>();
  for (const item of items) {
    const key = [item.urgency, item.gate.projectRef, item.gate.runRef].join(
      ":",
    );
    const group = groups.get(key) ?? {
      key,
      urgency: item.urgency,
      project: item.project,
      run: item.run,
      items: [],
    };
    group.items.push(item);
    groups.set(key, group);
  }
  return [...groups.values()];
}

export function projectArtifacts(
  artifacts: Artifact[],
  projectRef?: string,
): Artifact[] {
  return artifacts
    .filter((artifact) => !projectRef || artifact.projectRef === projectRef)
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt));
}
