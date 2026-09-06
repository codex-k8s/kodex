import type {
  Schedule,
  ScheduleInput,
  SchedulePreview,
  SchedulePreviewInput,
} from "@/shared/api/generated/openapi/types.gen";
import { normalizeTemplateVariable } from "@/features/agents/detail/model";

export function scheduleTimePreview(
  input: Pick<
    ScheduleInput,
    | "preset"
    | "cronExpression"
    | "timeOfDay"
    | "dayOfWeek"
    | "timezone"
    | "misfirePolicy"
    | "overlapPolicy"
  >,
): SchedulePreviewInput {
  return {
    preset: input.preset,
    ...(input.preset === "CUSTOM"
      ? { cronExpression: input.cronExpression?.trim() }
      : { timeOfDay: input.preset === "HOURLY" ? "00:00" : input.timeOfDay }),
    ...(input.preset === "WEEKLY" ? { dayOfWeek: input.dayOfWeek } : {}),
    timezone: input.timezone,
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: input.misfirePolicy,
    overlapPolicy: input.overlapPolicy,
    limit: 5,
  };
}

export function scheduleMaterializationInput(
  projectRef: string,
  input: ScheduleInput,
  mode: "DRAFT" | "CURRENT_REVISION",
  schedule?: Schedule,
  full = false,
): SchedulePreviewInput {
  if (
    !input.automationText.trim() ||
    !input.targetRef ||
    !input.name.trim() ||
    (mode === "CURRENT_REVISION" && !schedule)
  )
    throw new Error("Automation preview requires a task and target");
  return {
    ...scheduleTimePreview(input),
    materialization: {
      projectRef,
      targetType: input.targetType,
      targetRef: input.targetRef,
      name: input.name,
      task: input.automationText,
      input: { ...input.input },
      promptInputs: { ...input.promptInputs },
      sessionPolicy: input.sessionPolicy,
      notificationPolicy: input.notificationPolicy,
      ...(schedule
        ? {
            scheduleRef: schedule.ref,
            expectedScheduleVersion: schedule.version,
          }
        : {}),
      mode,
      includeFullMaterialization: full,
    },
  };
}

export function checkedScheduleMaterialization(
  response: SchedulePreview,
  request: SchedulePreviewInput,
  schedule?: Schedule,
): SchedulePreview {
  const target = request.materialization;
  const pin = response.materializationPin;
  const prompt = response.materializedPrompt;
  const context = prompt?.contextPin;
  if (
    !target ||
    !pin ||
    !prompt ||
    !context ||
    !response.automationVariables ||
    Array.from(response.automationVariables).length !== 6 ||
    !pin.executionActorRef ||
    !/^[a-f0-9]{64}$/.test(context.digest) ||
    pin.mode !== target.mode ||
    (pin.scheduleRef ?? "") !== (target.scheduleRef ?? "") ||
    pin.scheduleVersion !== (target.expectedScheduleVersion ?? 0) ||
    pin.timezone !== request.timezone ||
    !response.occurrences.length ||
    Date.parse(pin.scheduledFor) !==
      Date.parse(response.occurrences[0] ?? "") ||
    (!target.includeFullMaterialization &&
      prompt.fullMaterializedPrompt !== undefined)
  )
    throw new Error("Automation materialization context mismatch");
  if (
    schedule
      ? pin.baseRevisionRef !== schedule.currentRevision.ref ||
        pin.baseRevisionDigest !== schedule.currentRevision.digest
      : !!pin.baseRevisionRef || !!pin.baseRevisionDigest
  )
    throw new Error("Automation base revision mismatch");
  if (
    pin.mode === "DRAFT"
      ? pin.revisionAvailable || !!pin.revisionRef || !!pin.revisionDigest
      : !pin.revisionAvailable ||
        pin.revisionRef !== pin.baseRevisionRef ||
        pin.revisionDigest !== pin.baseRevisionDigest
  )
    throw new Error("Automation preview revision mode mismatch");
  if (
    target.targetType === "AGENT"
      ? context.agentRef !== target.targetRef || !!context.workflowRef
      : context.workflowRef !== target.targetRef ||
        !context.workflowRevisionRef ||
        context.workflowStageKey !== "workflow.coordinator.initial"
  )
    throw new Error("Automation execution target mismatch");
  if (
    pin.continuation
      ? !pin.sessionRef ||
        prompt.runtimeDiff?.sessionRef !== pin.sessionRef ||
        !context.previousRuntimeRevisionRef ||
        prompt.runtimeDiff.previousRevisionRef !==
          context.previousRuntimeRevisionRef
      : !!pin.sessionRef || !!prompt.runtimeDiff
  )
    throw new Error("Automation continuation context mismatch");
  const variables = response.automationVariables.map(normalizeTemplateVariable);
  const names = [
    "automation.ref",
    "automation.name",
    "automation.task",
    "automation.scheduled_at",
    "automation.timezone",
    "automation.revision",
  ];
  if (
    new Set(variables.map((item) => item.name)).size !== 6 ||
    variables.some((item) => !names.includes(item.name))
  )
    throw new Error("Automation variable catalog mismatch");
  const revision = variables.find(
    (item) => item.name === "automation.revision",
  );
  if (
    pin.revisionAvailable
      ? !revision?.available
      : revision?.available || revision?.reason !== "REVISION_NOT_SAVED"
  )
    throw new Error("Automation variable revision mismatch");
  return response;
}
