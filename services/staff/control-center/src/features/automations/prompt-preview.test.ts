import { describe, expect, it } from "vitest";
import type {
  ScheduleInput,
  SchedulePreview,
  TemplateVariable,
} from "@/shared/api/generated/openapi/types.gen";
import {
  scheduleMaterializationInput,
  scheduleTimePreview,
  checkedScheduleMaterialization,
} from "./prompt-preview";

const input: ScheduleInput = {
  name: "Сводка",
  targetType: "AGENT",
  targetRef: "agent",
  preset: "DAILY",
  timeOfDay: "09:00",
  timezone: "Europe/Saratov",
  automationText: "Подготовить отчёт",
  input: { task: "Отдельный исходный параметр", extra: { exact: true } },
  promptInputs: {},
  sessionPolicy: "NEW_EACH_RUN",
  notificationPolicy: "CONTROL_CENTER_ONLY",
  dstGapPolicy: "SHIFT_FORWARD",
  dstFoldPolicy: "RUN_ONCE_EARLIEST",
  misfirePolicy: "COALESCE",
  overlapPolicy: "FORBID",
};

describe("Automation materialization input", () => {
  it.each(["AGENT", "WORKFLOW"] as const)(
    "передаёт authoritative задачу и сохраняет input для %s",
    (targetType) => {
      const request = scheduleMaterializationInput(
        "project",
        { ...input, targetType },
        "DRAFT",
      );
      expect(request.materialization).toMatchObject({
        task: input.automationText,
        input: input.input,
        targetType,
        mode: "DRAFT",
        includeFullMaterialization: false,
      });
      expect(request.materialization).not.toHaveProperty("executionActorRef");
      expect(request.limit).toBe(5);
    },
  );
  it("не подменяет пустую задачу старым input.task", () => {
    expect(() =>
      scheduleMaterializationInput(
        "project",
        { ...input, automationText: "" },
        "DRAFT",
      ),
    ).toThrow("requires a task");
  });
  it("не придумывает текущую ревизию для новой автоматизации", () => {
    expect(() =>
      scheduleMaterializationInput("project", input, "CURRENT_REVISION"),
    ).toThrow();
  });
  it("использует один источник cron и ближайшие пять запусков", () => {
    expect(
      scheduleTimePreview({
        ...input,
        preset: "CUSTOM",
        cronExpression: " 0 9 * * * ",
      }),
    ).toMatchObject({ cronExpression: "0 9 * * *", limit: 5 });
    expect(scheduleTimePreview({ ...input, preset: "HOURLY" })).toMatchObject({
      timeOfDay: "00:00",
      limit: 5,
    });
  });
});

function response(): SchedulePreview {
  const variable = (name: string): TemplateVariable => ({
    name: `automation.${name}`,
    valueType: "STRING",
    description: name,
    example: "",
    source: "AUTOMATION",
    collection: false,
    available: name !== "revision",
    reason: name === "revision" ? "REVISION_NOT_SAVED" : "AVAILABLE",
    itemFields: [],
  });
  return {
    normalizedCronExpression: "0 9 * * *",
    occurrences: ["2026-09-07T05:00:00Z"],
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationVariables: [
      variable("ref"),
      variable("name"),
      variable("task"),
      variable("scheduled_at"),
      variable("timezone"),
      variable("revision"),
    ],
    materializationPin: {
      scheduleVersion: 0,
      scheduledFor: "2026-09-07T05:00:00Z",
      timezone: input.timezone,
      continuation: false,
      revisionAvailable: false,
      mode: "DRAFT",
      executionActorRef: "viewer",
    },
    materializedPrompt: {
      safePreview: input.automationText,
      complete: true,
      diagnostics: [],
      templateRef: "template",
      templateDigest: "a".repeat(64),
      materializationDigest: "a".repeat(64),
      effectiveCapabilities: [],
      serviceTemplateRevision: "1",
      serviceTemplateDigest: "a".repeat(64),
      variableSnapshotDigest: "a".repeat(64),
      locale: "ru",
      slots: [],
      sections: [],
      contextPin: { digest: "a".repeat(64), agentRef: input.targetRef },
    },
  };
}
describe("Automation materialization readback", () => {
  const request = scheduleMaterializationInput("project", input, "DRAFT");
  it("принимает будущую ревизию только с недоступной переменной revision", () => {
    expect(
      checkedScheduleMaterialization(response(), request).materializationPin
        ?.revisionAvailable,
    ).toBe(false);
  });
  it("не выдаёт незапрошенный полный текст", () => {
    const value = response();
    if (!value.materializedPrompt) throw new Error("Missing fixture prompt");
    value.materializedPrompt.fullMaterializedPrompt = "Full content";
    expect(() => checkedScheduleMaterialization(value, request)).toThrow(
      "context mismatch",
    );
  });
  it("отклоняет чужую цель", () => {
    const value = response();
    if (!value.materializedPrompt?.contextPin)
      throw new Error("Missing fixture context");
    value.materializedPrompt.contextPin.agentRef = "another_agent";
    expect(() => checkedScheduleMaterialization(value, request)).toThrow(
      "target mismatch",
    );
  });
  it("не принимает сохранённую ревизию под видом draft", () => {
    const value = response();
    if (!value.materializationPin) throw new Error("Missing fixture pin");
    value.materializationPin.revisionRef = "saved_revision";
    expect(() => checkedScheduleMaterialization(value, request)).toThrow(
      "revision mode mismatch",
    );
  });
  it("отклоняет continuation без exact Session diff", () => {
    const value = response();
    if (!value.materializationPin) throw new Error("Missing fixture pin");
    value.materializationPin.continuation = true;
    value.materializationPin.sessionRef = "session";
    expect(() => checkedScheduleMaterialization(value, request)).toThrow(
      "continuation context mismatch",
    );
  });
});
