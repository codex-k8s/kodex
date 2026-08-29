import { describe, expect, it } from "vitest";

import {
  scheduleCapabilities,
  scheduleInput,
  verifyScheduleCommandReadback,
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
    nextActions: ["EDIT", "DISABLE"],
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
    const mutation = schedule({ version: 4 });
    expect(
      verifyScheduleReadback(submitted, mutation, schedule({ version: 4 })),
    ).toEqual(schedule({ version: 4 }));

    expect(() =>
      verifyScheduleReadback(
        submitted,
        mutation,
        schedule({ version: 4, name: "Другое имя" }),
      ),
    ).toThrow(AppProblem);

    expect(
      verifyScheduleReadback(
        submitted,
        mutation,
        schedule({
          version: 4,
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

  it("не путает права редактирования и архивации", () => {
    expect(scheduleCapabilities(schedule({ nextActions: ["EDIT"] }))).toEqual({
      canEdit: true,
      canPause: false,
      canEnable: false,
      canArchive: false,
      canDeletePermanently: false,
    });
    expect(
      scheduleCapabilities(schedule({ nextActions: ["ARCHIVE"] })),
    ).toMatchObject({ canEdit: false, canArchive: true });
  });
});
