import { describe, expect, it } from "vitest";

import {
  scheduleCanBeDeleted,
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

  it("проверяет readback команды и закрывает удаление без API", () => {
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
    expect(scheduleCanBeDeleted()).toBe(false);
  });

  it("не принимает неизвестный preset как изменяемый ScheduleInput", () => {
    expect(() => scheduleInput(schedule({ preset: "0 9 * * *" }))).toThrow(
      AppProblem,
    );
  });
});
