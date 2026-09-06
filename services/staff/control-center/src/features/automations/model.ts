import type {
  Schedule,
  ScheduleInput,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

export interface ScheduleCapabilities {
  canEdit: boolean;
  canPause: boolean;
  canEnable: boolean;
  canArchive: boolean;
  canDelete: boolean;
}

export type ScheduleFilter = "CURRENT" | "ALL" | Schedule["state"];

export function scheduleCapabilities(schedule: Schedule): ScheduleCapabilities {
  return {
    canEdit: schedule.nextActions.includes("EDIT"),
    canPause: schedule.nextActions.includes("DISABLE"),
    canEnable: schedule.nextActions.includes("ENABLE"),
    canArchive: schedule.nextActions.includes("ARCHIVE"),
    canDelete: schedule.nextActions.includes("DELETE"),
  };
}

export function scheduleMatchesFilter(
  schedule: Schedule,
  filter: ScheduleFilter,
): boolean {
  if (filter === "ALL") return true;
  if (filter === "CURRENT")
    return schedule.state !== "ARCHIVED" && schedule.state !== "DELETED";
  return schedule.state === filter;
}

export function scheduleInput(schedule: Schedule): ScheduleInput {
  const targetType = schedule.target.type;
  if (!isSchedulePreset(schedule.preset))
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_PRESET_UNSUPPORTED",
      retryable: false,
      kind: "unavailable",
    });
  const dayOfWeek = schedule.dayOfWeek || undefined;
  return {
    name: schedule.name,
    targetRef: schedule.target.ref,
    targetType,
    preset: schedule.preset,
    ...(schedule.preset === "CUSTOM"
      ? { cronExpression: schedule.cronExpression }
      : {}),
    timeOfDay: schedule.timeOfDay ?? "00:00",
    ...(dayOfWeek ? { dayOfWeek } : {}),
    timezone: schedule.timezone,
    input: { ...schedule.input },
    sessionPolicy: schedule.sessionPolicy,
    notificationPolicy: schedule.notificationPolicy,
    dstGapPolicy: schedule.dstGapPolicy,
    dstFoldPolicy: schedule.dstFoldPolicy,
    misfirePolicy: schedule.misfirePolicy,
    overlapPolicy: schedule.overlapPolicy,
    automationText: schedule.automationText,
    promptInputs: { ...schedule.promptInputs },
  };
}

export function isSchedulePreset(
  value: string,
): value is ScheduleInput["preset"] {
  return ["HOURLY", "DAILY", "WEEKDAYS", "WEEKLY", "CUSTOM"].includes(value);
}

function canonicalJson(value: unknown): string {
  function canonical(current: unknown): unknown {
    if (Array.isArray(current)) return current.map(canonical);
    if (typeof current !== "object" || current === null) return current;
    return Object.fromEntries(
      Object.entries(current)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, item]) => [key, canonical(item)]),
    );
  }
  return JSON.stringify(canonical(value));
}

function sameInput(left: ScheduleInput, right: ScheduleInput): boolean {
  return (
    left.name === right.name &&
    left.targetRef === right.targetRef &&
    left.targetType === right.targetType &&
    left.preset === right.preset &&
    (left.preset !== "CUSTOM" ||
      left.cronExpression === right.cronExpression) &&
    left.timeOfDay === right.timeOfDay &&
    (left.dayOfWeek ?? "") === (right.dayOfWeek ?? "") &&
    left.timezone === right.timezone &&
    left.sessionPolicy === right.sessionPolicy &&
    left.notificationPolicy === right.notificationPolicy &&
    Object.is(left.dstGapPolicy, right.dstGapPolicy) &&
    Object.is(left.dstFoldPolicy, right.dstFoldPolicy) &&
    left.misfirePolicy === right.misfirePolicy &&
    left.overlapPolicy === right.overlapPolicy &&
    left.automationText === right.automationText &&
    canonicalJson(left.promptInputs) === canonicalJson(right.promptInputs) &&
    canonicalJson(left.input) === canonicalJson(right.input)
  );
}

export function verifyScheduleReadback(
  submitted: ScheduleInput,
  mutationResult: Schedule,
  readback: Schedule | undefined,
): Schedule {
  if (
    !readback ||
    readback.ref !== mutationResult.ref ||
    readback.version < mutationResult.version ||
    !sameInput(scheduleInput(readback), submitted) ||
    readback.currentRevision.revision <
      mutationResult.currentRevision.revision ||
    !revisionMatchesInput(readback.currentRevision, submitted)
  )
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_READBACK_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return readback;
}

export function verifyScheduleCommandReadback(
  mutationResult: Schedule,
  readback: Schedule | undefined,
): Schedule {
  if (
    !readback ||
    readback.ref !== mutationResult.ref ||
    readback.version < mutationResult.version ||
    readback.state !== mutationResult.state ||
    readback.currentRevision.ref !== mutationResult.currentRevision.ref ||
    readback.currentRevision.revision !==
      mutationResult.currentRevision.revision
  )
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_READBACK_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return readback;
}

export function verifyScheduleDeleteReadback(
  mutationResult: Schedule,
  readback: { kind: "found"; schedule: Schedule } | { kind: "not-found" },
): Schedule {
  const result =
    readback.kind === "found"
      ? verifyScheduleCommandReadback(mutationResult, readback.schedule)
      : mutationResult;
  if (result.state !== "DELETED" || result.nextActions.includes("DELETE"))
    throw new AppProblem({
      status: 502,
      code: "SCHEDULE_DELETE_READBACK_MISMATCH",
      retryable: true,
      kind: "unavailable",
    });
  return result;
}

function revisionMatchesInput(
  revision: Schedule["currentRevision"],
  input: ScheduleInput,
): boolean {
  return (
    revision.name === input.name &&
    revision.target.ref === input.targetRef &&
    revision.target.type === input.targetType &&
    revision.preset === input.preset &&
    revision.timezone === input.timezone &&
    revision.sessionPolicy === input.sessionPolicy &&
    revision.notificationPolicy === input.notificationPolicy &&
    Object.is(revision.dstGapPolicy, input.dstGapPolicy) &&
    Object.is(revision.dstFoldPolicy, input.dstFoldPolicy) &&
    revision.misfirePolicy === input.misfirePolicy &&
    revision.overlapPolicy === input.overlapPolicy &&
    revision.automationText === input.automationText &&
    canonicalJson(revision.promptInputs) ===
      canonicalJson(input.promptInputs) &&
    canonicalJson(revision.input) === canonicalJson(input.input)
  );
}
