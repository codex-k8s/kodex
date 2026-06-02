import type { AgentRunRuntimeStatus, AgentRunStatus, AgentRunSummary, AgentSessionSummary } from '@/shared/api/generated';

export type StatusTone = 'neutral' | 'success' | 'warning' | 'error' | 'info';

const liveRunStatuses: AgentRunStatus[] = ['requested', 'starting', 'running'];
const problemRunStatuses: AgentRunStatus[] = ['failed', 'cancelled'];

export function runIsLive(run: AgentRunSummary): boolean {
  return liveRunStatuses.includes(run.status);
}

export function runIsWaiting(run: AgentRunSummary): boolean {
  return run.status === 'waiting' || run.human_gate_waiting || run.follow_up_waiting;
}

export function runHasProblem(run: AgentRunSummary): boolean {
  return (
    problemRunStatuses.includes(run.status) ||
    run.runtime_observation_state === 'unavailable' ||
    run.runtime_observation_state === 'conflict' ||
    Boolean(run.failure_code || run.runtime_safe_error_code || run.latest_activity?.bounded_error)
  );
}

export function runIsCompleted(run: AgentRunSummary): boolean {
  return run.status === 'completed';
}

export function runPrimarySummary(run: AgentRunSummary): string | undefined {
  return firstText(
    run.result_summary,
    run.runtime_safe_summary,
    run.latest_activity?.safe_summary,
    run.latest_activity?.bounded_error,
    run.runtime_job_ref,
  );
}

export function runProblemCode(run: AgentRunSummary): string | undefined {
  return firstText(run.failure_code, run.runtime_safe_error_code, run.latest_activity?.bounded_error);
}

export function runWaitingCode(run: AgentRunSummary): string | undefined {
  if (run.human_gate_waiting) {
    return firstText(run.human_gate_reason_code, run.human_gate_request_ref, 'human_gate');
  }
  if (run.follow_up_waiting) {
    return 'follow_up';
  }
  if (run.status === 'waiting') {
    return firstText(run.latest_activity?.safe_summary, run.latest_activity?.activity_kind, 'waiting');
  }
  return undefined;
}

export function sessionPrimarySummary(session: AgentSessionSummary): string | undefined {
  return firstText(
    session.latest_run_safe_summary,
    session.latest_activity?.safe_summary,
    session.provider_work_item_ref,
    session.latest_runtime_job_ref,
  );
}

export function sessionWaitingCode(session: AgentSessionSummary): string | undefined {
  if (session.human_gate_waiting) {
    return firstText(session.human_gate_reason_code, session.human_gate_request_ref, 'human_gate');
  }
  if (session.follow_up_waiting) {
    return firstText(session.follow_up_ref, 'follow_up');
  }
  if (session.status === 'waiting') {
    return firstText(session.latest_activity?.safe_summary, session.latest_activity?.activity_kind, 'waiting');
  }
  return undefined;
}

export function runtimeStatusHasProblem(status?: AgentRunRuntimeStatus): boolean {
  if (!status) {
    return false;
  }
  return (
    status.run_status === 'failed' ||
    status.run_status === 'cancelled' ||
    status.runtime_job_status === 'failed' ||
    status.runtime_job_status === 'cancelled' ||
    status.runtime_job_status === 'timed_out' ||
    status.observation_state === 'unavailable' ||
    status.observation_state === 'conflict' ||
    Boolean(status.safe_error_code)
  );
}

export function runtimeStatusIsWaiting(status?: AgentRunRuntimeStatus): boolean {
  if (!status) {
    return false;
  }
  return status.run_status === 'waiting' || status.human_gate_waiting || status.follow_up_waiting;
}

export function statusTone(status?: string): StatusTone {
  if (status === 'succeeded' || status === 'completed') {
    return 'success';
  }
  if (status === 'running' || status === 'started' || status === 'waiting' || status === 'pending') {
    return 'warning';
  }
  if (status === 'failed' || status === 'cancelled' || status === 'timed_out') {
    return 'error';
  }
  if (status === 'requested' || status === 'starting' || status === 'claimed' || status === 'live') {
    return 'info';
  }
  return 'neutral';
}

function firstText(...values: Array<string | undefined>): string | undefined {
  return values.find((value) => value !== undefined && value.trim().length > 0);
}
