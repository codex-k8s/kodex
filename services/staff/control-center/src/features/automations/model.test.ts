import { describe, expect, it } from "vitest";

import {
  scheduleCapabilities,
  scheduleInput,
  scheduleMatchesFilter,
  verifyScheduleCommandReadback,
  verifyScheduleDeleteReadback,
  verifyScheduleReadback,
} from "@/features/automations/model";
import type { Schedule } from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

function schedule(options: Partial<Schedule> = {}): Schedule {
  return {
    ref: "schedule_daily",
    version: 3,
    projectRef: "project_sales",
    name: "Ежедневная сводка",
    target: {
      type: "AGENT",
      ref: "agent_sales",
      displayName: "Аналитик продаж",
      version: 2,
    },
    state: "ACTIVE",
    preset: "WEEKDAYS",
    timeOfDay: "09:00",
    timezone: "Europe/Saratov",
    input: { task: "Собрать сводку", retained: { exact: true } },
    sessionPolicy: "NEW_EACH_RUN",
    notificationPolicy: "CONTROL_CENTER_ONLY",
    currentRevision: {
      ref: "schedule_revision_3",
      revision: 3,
      digest: "a".repeat(64),
      name: "Ежедневная сводка",
      target: {
        type: "AGENT",
        ref: "agent_sales",
        displayName: "Аналитик продаж",
        version: 2,
      },
      preset: "WEEKDAYS",
      cronExpression: "0 9 * * 1-5",
      timezone: "Europe/Saratov",
      input: { task: "Собрать сводку", retained: { exact: true } },
      sessionPolicy: "NEW_EACH_RUN",
      notificationPolicy: "CONTROL_CENTER_ONLY",
      createdAt: "2026-08-30T06:00:00Z",
    },
    nextActions: ["EDIT", "DISABLE", "DELETE"],
    ...options,
  };
}

describe("automations model", () => {
  it("строит update input без потери неизвестных полей задачи", () => {
    expect(scheduleInput(schedule())).toEqual({
      name: "Ежедневная сводка",
      targetRef: "agent_sales",
      targetType: "AGENT",
      preset: "WEEKDAYS",
      timeOfDay: "09:00",
      timezone: "Europe/Saratov",
      input: { task: "Собрать сводку", retained: { exact: true } },
      sessionPolicy: "NEW_EACH_RUN",
      notificationPolicy: "CONTROL_CENTER_ONLY",
    });
  });

  it("принимает только совпавший authoritative readback", () => {
    const submitted = scheduleInput(schedule());
    const mutation = schedule({
      version: 4,
      currentRevision: {
        ...schedule().currentRevision,
        ref: "schedule_revision_4",
        revision: 4,
      },
    });
    expect(verifyScheduleReadback(submitted, mutation, mutation)).toEqual(
      mutation,
    );

    expect(() =>
      verifyScheduleReadback(
        submitted,
        mutation,
        schedule({
          version: 4,
          name: "Другое имя",
          currentRevision: mutation.currentRevision,
        }),
      ),
    ).toThrow(AppProblem);

    expect(
      verifyScheduleReadback(
        submitted,
        mutation,
        schedule({
          version: 4,
          currentRevision: mutation.currentRevision,
          input: { retained: { exact: true }, task: "Собрать сводку" },
        }),
      ).version,
    ).toBe(4);
  });

  it("проверяет authoritative readback команды, включая архивацию", () => {
    const mutation = schedule({ state: "PAUSED", version: 4 });
    expect(
      verifyScheduleCommandReadback(
        mutation,
        schedule({ state: "PAUSED", version: 4 }),
      ).state,
    ).toBe("PAUSED");
    expect(() =>
      verifyScheduleCommandReadback(
        mutation,
        schedule({ state: "ACTIVE", version: 5 }),
      ),
    ).toThrow(AppProblem);
    expect(
      verifyScheduleCommandReadback(
        schedule({ state: "ARCHIVED", version: 5 }),
        schedule({ state: "ARCHIVED", version: 5, nextActions: ["OPEN"] }),
      ).state,
    ).toBe("ARCHIVED");
  });

  it("не принимает неизвестный preset как изменяемый ScheduleInput", () => {
    expect(() => scheduleInput(schedule({ preset: "0 9 * * *" }))).toThrow(
      AppProblem,
    );
  });

  it("принимает terminal delete только после authoritative DELETED readback", () => {
    const deleted = schedule({
      state: "DELETED",
      version: 5,
      nextActions: ["OPEN"],
    });
    expect(
      verifyScheduleDeleteReadback(deleted, {
        kind: "found",
        schedule: deleted,
      }).state,
    ).toBe("DELETED");
    expect(
      verifyScheduleDeleteReadback(deleted, { kind: "not-found" }).state,
    ).toBe("DELETED");
    expect(() =>
      verifyScheduleDeleteReadback(deleted, {
        kind: "found",
        schedule: schedule({
          state: "ARCHIVED",
          version: 5,
          nextActions: ["OPEN"],
        }),
      }),
    ).toThrow(AppProblem);
  });

  it("не путает права редактирования и архивации", () => {
    expect(scheduleCapabilities(schedule({ nextActions: ["EDIT"] }))).toEqual({
      canEdit: true,
      canPause: false,
      canEnable: false,
      canArchive: false,
      canDelete: false,
    });
    expect(
      scheduleCapabilities(schedule({ nextActions: ["ARCHIVE"] })),
    ).toMatchObject({ canEdit: false, canArchive: true });
  });

  it("строит lifecycle только из authoritative nextActions", () => {
    expect(
      scheduleCapabilities(
        schedule({
          state: "ACTIVE",
          nextActions: ["EDIT", "DISABLE", "ARCHIVE", "DELETE"],
        }),
      ),
    ).toEqual({
      canEdit: true,
      canPause: true,
      canEnable: false,
      canArchive: true,
      canDelete: true,
    });
    expect(
      scheduleCapabilities(
        schedule({
          state: "PAUSED",
          nextActions: ["EDIT", "ENABLE", "ARCHIVE"],
        }),
      ),
    ).toEqual({
      canEdit: true,
      canPause: false,
      canEnable: true,
      canArchive: true,
      canDelete: false,
    });
    expect(
      scheduleCapabilities(
        schedule({ state: "ARCHIVED", nextActions: ["OPEN"] }),
      ),
    ).toEqual({
      canEdit: false,
      canPause: false,
      canEnable: false,
      canArchive: false,
      canDelete: false,
    });
  });

  it("отделяет действующие автоматизации от read-only архива", () => {
    expect(
      scheduleMatchesFilter(schedule({ state: "ACTIVE" }), "CURRENT"),
    ).toBe(true);
    expect(
      scheduleMatchesFilter(schedule({ state: "PAUSED" }), "CURRENT"),
    ).toBe(true);
    expect(
      scheduleMatchesFilter(schedule({ state: "ARCHIVED" }), "CURRENT"),
    ).toBe(false);
    expect(
      scheduleMatchesFilter(schedule({ state: "ARCHIVED" }), "ARCHIVED"),
    ).toBe(true);
    expect(scheduleMatchesFilter(schedule({ state: "ARCHIVED" }), "ALL")).toBe(
      true,
    );
    expect(
      scheduleMatchesFilter(schedule({ state: "DELETED" }), "CURRENT"),
    ).toBe(false);
    expect(
      scheduleMatchesFilter(schedule({ state: "DELETED" }), "DELETED"),
    ).toBe(true);
  });
});
