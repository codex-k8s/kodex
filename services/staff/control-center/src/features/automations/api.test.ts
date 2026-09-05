import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Run,
  Schedule,
  ScheduleInput,
  ScheduleRevision,
  ScheduleRunOccurrence,
} from "@/shared/api/generated/openapi/types.gen";

const mocks = vi.hoisted(() => ({
  command: vi.fn(),
  create: vi.fn(),
  delete: vi.fn(),
  get: vi.fn(),
  list: vi.fn(),
  revisions: vi.fn(),
  runs: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  commandSchedule: mocks.command,
  createSchedule: mocks.create,
  deleteSchedule: mocks.delete,
  getSchedule: mocks.get,
  listScheduleRevisions: mocks.revisions,
  listScheduleRuns: mocks.runs,
  listSchedules: mocks.list,
  updateSchedule: mocks.update,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import {
  commandSchedule,
  loadSchedulePage,
  loadScheduleRevisionPage,
  loadScheduleRunPage,
  removeSchedule,
  saveSchedule,
} from "./api";

const input: ScheduleInput = {
  name: "Ежедневная сводка",
  targetRef: "agent_sales",
  targetType: "AGENT",
  preset: "DAILY",
  timeOfDay: "09:00",
  timezone: "Europe/Saratov",
  input: { task: "Подготовить сводку" },
  sessionPolicy: "NEW_EACH_RUN",
  notificationPolicy: "CONTROL_CENTER_ONLY",
  dstGapPolicy: "SHIFT_FORWARD",
  dstFoldPolicy: "RUN_ONCE_EARLIEST",
  misfirePolicy: "COALESCE",
  overlapPolicy: "FORBID",
  automationText: "",
  promptInputs: {},
};

function revision(value = 4): ScheduleRevision {
  return {
    ref: `schedule_revision_${String(value)}`,
    revision: value,
    digest: String(value).repeat(64),
    targetVersion: 2,
    targetDigest: "b".repeat(64),
    name: input.name,
    target: {
      type: "AGENT",
      ref: input.targetRef,
      displayName: "Аналитик продаж",
      version: 2,
    },
    preset: input.preset,
    cronExpression: "0 9 * * *",
    timezone: input.timezone,
    input: input.input,
    sessionPolicy: input.sessionPolicy,
    notificationPolicy: input.notificationPolicy,
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationText: "",
    promptInputs: {},
    createdAt: "2026-08-30T06:00:00Z",
  };
}

function schedule(overrides: Partial<Schedule> = {}): Schedule {
  return {
    ref: "schedule_daily",
    version: 4,
    projectRef: "project_sales",
    name: input.name,
    target: revision().target,
    state: "ACTIVE",
    preset: input.preset,
    timeOfDay: input.timeOfDay,
    timezone: input.timezone,
    input: input.input,
    sessionPolicy: input.sessionPolicy,
    notificationPolicy: input.notificationPolicy,
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: "COALESCE",
    overlapPolicy: "FORBID",
    automationText: "",
    promptInputs: {},
    targetVersion: 2,
    targetDigest: "b".repeat(64),
    cronExpression: "0 9 * * *",
    currentRevision: revision(),
    nextActions: ["EDIT", "DISABLE", "ARCHIVE", "DELETE"],
    ...overrides,
  };
}

function response<T>(data: T, status = 200) {
  return { data, response: new Response(null, { status }) };
}

function occurrence(
  overrides: Partial<ScheduleRunOccurrence> = {},
): ScheduleRunOccurrence {
  return {
    scheduleRef: "schedule_daily",
    scheduleRevisionRef: "schedule_revision_4",
    scheduleRevision: 4,
    run: { ref: "run_1" } as Run,
    ...overrides,
  };
}

describe("automation API boundary", () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset());
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
  });

  it("передаёт server-side query и cursor списка", async () => {
    mocks.list.mockResolvedValue(
      response({ items: [schedule()], nextPageToken: "next_schedule" }),
    );

    const page = await loadSchedulePage(
      "project_sales",
      "  сводка  ",
      "cursor_schedule",
    );

    expect(page.nextPageToken).toBe("next_schedule");
    expect(mocks.list).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { projectRef: "project_sales" },
        query: {
          pageSize: 40,
          pageToken: "cursor_schedule",
          query: "сводка",
        },
      }),
    );
  });

  it("обновляет по OCC и возвращает только authoritative GET readback", async () => {
    const current = schedule();
    const updated = schedule({
      version: 5,
      targetVersion: 2,
      targetDigest: "b".repeat(64),
      cronExpression: "0 9 * * *",
      currentRevision: revision(5),
    });
    mocks.update.mockResolvedValue(response(updated));
    mocks.get.mockResolvedValue(response(updated));

    const result = await saveSchedule(current.projectRef, input, current);

    expect(result).toEqual(updated);
    expect(mocks.update.mock.calls[0]?.[0]).toMatchObject({
      body: input,
      headers: { "If-Match": '"4"' },
      path: { scheduleRef: current.ref },
    });
    expect(mocks.get).toHaveBeenCalledWith(
      expect.objectContaining({ path: { scheduleRef: current.ref } }),
    );
  });

  it("отклоняет несовпавший readback после update", async () => {
    const updated = schedule({
      version: 5,
      targetVersion: 2,
      targetDigest: "b".repeat(64),
      cronExpression: "0 9 * * *",
      currentRevision: revision(5),
    });
    mocks.update.mockResolvedValue(response(updated));
    mocks.get.mockResolvedValue(
      response(schedule({ version: 5, name: "Чужая автоматизация" })),
    );

    await expect(
      saveSchedule("project_sales", input, schedule()),
    ).rejects.toMatchObject({ code: "SCHEDULE_READBACK_MISMATCH" });
  });

  it("выполняет lifecycle-команду с OCC и сохраняет immutable revision", async () => {
    const paused = schedule({
      version: 5,
      state: "PAUSED",
      nextActions: ["EDIT", "ENABLE", "ARCHIVE", "DELETE"],
    });
    mocks.command.mockResolvedValue(response(paused));
    mocks.get.mockResolvedValue(response(paused));

    const result = await commandSchedule(schedule(), "PAUSE");

    expect(result.state).toBe("PAUSED");
    expect(result.currentRevision.ref).toBe(schedule().currentRevision.ref);
    expect(mocks.command.mock.calls[0]?.[0]).toMatchObject({
      body: { action: "PAUSE" },
      headers: { "If-Match": '"4"' },
    });
  });

  it("terminal delete подтверждает DELETED через exact readback", async () => {
    const deleted = schedule({
      version: 5,
      state: "DELETED",
      nextActions: ["OPEN"],
    });
    mocks.delete.mockResolvedValue(response(deleted));
    mocks.get.mockResolvedValue({
      error: { code: "NOT_FOUND", status: 404 },
      response: new Response(undefined, { status: 404 }),
    });

    const result = await removeSchedule(schedule());

    expect(result.state).toBe("DELETED");
    expect(mocks.delete.mock.calls[0]?.[0]).toMatchObject({
      headers: { "If-Match": '"4"' },
      path: { scheduleRef: "schedule_daily" },
    });
  });

  it("страницы revisions и runs запрашивает по exact schedule ref и cursor", async () => {
    mocks.revisions.mockResolvedValue(
      response({ items: [revision()], nextPageToken: "revision_cursor" }),
    );
    mocks.runs.mockResolvedValue(
      response({ items: [occurrence()], nextPageToken: "run_cursor" }),
    );

    await loadScheduleRevisionPage("schedule_daily", "revision_before");
    const page = await loadScheduleRunPage("schedule_daily", "run_before");

    expect(mocks.revisions).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { scheduleRef: "schedule_daily" },
        query: { pageSize: 40, pageToken: "revision_before" },
      }),
    );
    expect(mocks.runs).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { scheduleRef: "schedule_daily" },
        query: { pageSize: 40, pageToken: "run_before" },
      }),
    );
    expect(page.items[0]).toMatchObject({
      scheduleRef: "schedule_daily",
      scheduleRevisionRef: "schedule_revision_4",
      scheduleRevision: 4,
      run: { ref: "run_1" },
    });
  });

  it("закрыто отклоняет run occurrence другой автоматики или без exact revision", async () => {
    mocks.runs.mockResolvedValueOnce(
      response({
        items: [occurrence({ scheduleRef: "schedule_other" })],
        nextPageToken: "",
      }),
    );

    await expect(loadScheduleRunPage("schedule_daily")).rejects.toMatchObject({
      code: "SCHEDULE_RUN_OCCURRENCE_MISMATCH",
    });

    mocks.runs.mockResolvedValueOnce(
      response({
        items: [occurrence({ scheduleRevisionRef: "" })],
        nextPageToken: "",
      }),
    );

    await expect(loadScheduleRunPage("schedule_daily")).rejects.toMatchObject({
      code: "SCHEDULE_RUN_OCCURRENCE_MISMATCH",
    });
  });
});
