import type { AppProblem } from "@/shared/api/problem";

export type RemotePhase =
  | "idle"
  | "loading"
  | "ready"
  | "empty"
  | "error"
  | "forbidden"
  | "conflict";

export interface RemoteState<T> {
  phase: RemotePhase;
  data: T;
  problem: AppProblem | null;
  requestVersion: number;
}

export function remoteState<T>(initial: T): RemoteState<T> {
  return { phase: "idle", data: initial, problem: null, requestVersion: 0 };
}

export function beginRequest<T>(state: RemoteState<T>): number {
  state.requestVersion += 1;
  state.phase = "loading";
  state.problem = null;
  return state.requestVersion;
}

export function acceptRequest<T>(
  state: RemoteState<T>,
  version: number,
): boolean {
  return state.requestVersion === version;
}

export function finishRequest<T>(
  state: RemoteState<T>,
  version: number,
  data: T,
  empty: boolean,
): void {
  if (!acceptRequest(state, version)) return;
  state.data = data;
  state.phase = empty ? "empty" : "ready";
  state.problem = null;
}

export function failRequest<T>(
  state: RemoteState<T>,
  version: number,
  problem: AppProblem,
): void {
  if (!acceptRequest(state, version)) return;
  state.problem = problem;
  state.phase =
    problem.kind === "forbidden"
      ? "forbidden"
      : problem.kind === "conflict"
        ? "conflict"
        : "error";
}

export function invalidate<T>(state: RemoteState<T>): void {
  state.requestVersion += 1;
}

export function resetRemoteState<T>(state: RemoteState<T>, initial: T): void {
  state.requestVersion += 1;
  state.phase = "idle";
  state.data = initial;
  state.problem = null;
}
