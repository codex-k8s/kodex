import { createSSRApp, h } from "vue";
import { createI18n } from "vue-i18n";
import { renderToString } from "@vue/server-renderer";
import { describe, expect, it } from "vitest";

import AutomationEditorDialog from "@/features/automations/AutomationEditorDialog.vue";
import type { Agent, Schedule } from "@/shared/api/generated/openapi/types.gen";

const agent = {
  ref: "agent_sales",
  name: "Аналитик продаж",
  enabled: true,
  system: false,
} as Agent;

const schedule = {
  ref: "schedule_daily",
  version: 7,
  projectRef: "project_sales",
  name: "Ежедневная сводка",
  target: {
    type: "AGENT",
    ref: agent.ref,
    displayName: agent.name,
    version: 2,
  },
  state: "PAUSED",
  preset: "WEEKLY",
  timeOfDay: "09:30",
  dayOfWeek: "FRIDAY",
  timezone: "Europe/Saratov",
  input: { task: "Подготовить сводку", retained: { exact: true } },
  sessionPolicy: "CONTINUE_ONE",
  notificationPolicy: "CONTROL_CENTER_AND_OPTIONAL_CHANNELS",
  dstGapPolicy: "SHIFT_FORWARD",
  dstFoldPolicy: "RUN_ONCE_EARLIEST",
  misfirePolicy: "COALESCE",
  overlapPolicy: "FORBID",
  automationText: "",
  promptInputs: {},
  targetVersion: 2,
  targetDigest: "b".repeat(64),
  cronExpression: "0 9 * * *",
  currentRevision: {
    ref: "schedule_revision_7",
    revision: 7,
    digest: "a".repeat(64),
    targetVersion: 2,
    targetDigest: "b".repeat(64),
    name: "Ежедневная сводка",
    target: {
      type: "AGENT",
      ref: agent.ref,
      displayName: agent.name,
      version: 2,
    },
    preset: "WEEKLY",
    cronExpression: "30 9 * * 5",
    timezone: "Europe/Saratov",
    input: { task: "Подготовить сводку", retained: { exact: true } },
    sessionPolicy: "CONTINUE_ONE",
    notificationPolicy: "CONTROL_CENTER_AND_OPTIONAL_CHANNELS",
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationText: "",
    promptInputs: {},
    createdAt: "2026-08-30T06:00:00Z",
  },
  nextActions: ["EDIT", "ENABLE", "ARCHIVE"],
} as Schedule;

function messages() {
  return {
    common: {
      cancel: "Отмена",
      close: "Закрыть",
      create: "Создать",
      name: "Название",
      save: "Сохранить",
      target: "Цель",
    },
    automations: {
      chooseTarget: "Выберите цель",
      controlCenterOnly: "Только Control Center",
      continueSession: "Продолжать одну сессию",
      daily: "Каждый день",
      day: {
        MONDAY: "Понедельник",
        TUESDAY: "Вторник",
        WEDNESDAY: "Среда",
        THURSDAY: "Четверг",
        FRIDAY: "Пятница",
        SATURDAY: "Суббота",
        SUNDAY: "Воскресенье",
      },
      dayOfWeek: "День недели",
      hourly: "Каждый час",
      new: "Новая автоматизация",
      newSession: "Новая сессия",
      notifications: "Уведомления",
      optionalChannels: "Control Center и подключённые каналы",
      preset: "Когда запускать",
      sessionPolicy: "Политика сессии",
      timeOfDay: "Время запуска",
      timezone: "Часовой пояс",
      weekdays: "По рабочим дням",
      weekly: "Раз в неделю",
    },
  };
}

describe("AutomationEditorDialog", () => {
  it("открывает существующую автоматизацию со всей изменяемой конфигурацией", async () => {
    const app = createSSRApp({
      render: () =>
        h(AutomationEditorDialog, {
          projectRef: "project_sales",
          schedule,
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: { ru: messages() },
      }),
    );

    const html = await renderToString(app);

    expect(html).toContain("Изменить автоматизацию");
    expect(html).toContain('value="Ежедневная сводка"');
    expect(html).not.toContain("Подготовить сводку</textarea>");
    expect(html).toContain('<select value="AGENT"');
    expect(html).toMatch(/<option value="WEEKLY"[^>]*selected>/);
    expect(html).toMatch(/<option value="FRIDAY"[^>]*selected>/);
    expect(html).toMatch(/<option value="CONTINUE_ONE"[^>]*selected>/);
    expect(html).toMatch(
      /<option value="CONTROL_CENTER_AND_OPTIONAL_CHANNELS"[^>]*selected>/,
    );
    expect(html).toContain("версии 7");
    expect(html).toContain("Аналитик продаж");
    expect(html).not.toContain("agent_sales");
    expect(html).toContain("Сохранить");
  });
});
