import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  Schedule,
  ScheduleInput,
} from "@/shared/api/generated/openapi/types.gen";

const updateScheduleMock = vi.hoisted(() => vi.fn());
const commandScheduleMock = vi.hoisted(() => vi.fn());
const listSchedulesMock = vi.hoisted(() => vi.fn());

vi.mock("@/shared/api/generated/openapi/sdk.gen", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  updateSchedule: updateScheduleMock,
  commandSchedule: commandScheduleMock,
  listSchedules: listSchedulesMock,
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));

import { usePlatformStore } from "@/features/platform/store";

const input: ScheduleInput = {
  name: "Ежедневная сводка",
  targetType: "AGENT",
  targetRef: "agent_sales",
  preset: "DAILY",
  timeOfDay: "09:00",
  timezone: "Europe/Saratov",
  input: { task: "Подготовить сводку" },
  sessionPolicy: "NEW_EACH_RUN",
  notificationPolicy: "CONTROL_CENTER_ONLY",
};

function schedule(overrides: Partial<Schedule> = {}): Schedule {
  return {
    ref: "schedule_daily",
    version: 4,
    projectRef: "project_sales",
    name: input.name,
    target: {
      type: "AGENT",
      ref: input.targetRef,
      displayName: "Аналитик продаж",
      version: 2,
    },
    state: "ACTIVE",
    preset: input.preset,
    timeOfDay: input.timeOfDay,
    timezone: input.timezone,
    input: input.input,
    sessionPolicy: input.sessionPolicy,
    notificationPolicy: input.notificationPolicy,
    currentRevision: {
      ref: "schedule_revision_4",
      revision: 4,
      digest: "a".repeat(64),
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
      createdAt: "2026-08-30T06:00:00Z",
    },
    nextActions: ["EDIT", "DISABLE", "ARCHIVE"],
    ...overrides,
  };
}

describe("automation store boundary", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    updateScheduleMock.mockReset();
    commandScheduleMock.mockReset();
    listSchedulesMock.mockReset();
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
  });

  it("обновляет существующую автоматизацию через versioned update path", async () => {
    const current = schedule();
    const updated = schedule({ version: 5, name: "Утренняя сводка" });
    updateScheduleMock.mockResolvedValue({
      data: updated,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await store.saveSchedule(
      current.projectRef,
      { ...input, name: updated.name },
      current,
    );

    const request = updateScheduleMock.mock.calls[0]?.[0] as
      | Parameters<
          typeof import("@/shared/api/generated/openapi/sdk.gen").updateSchedule
        >[0]
      | undefined;
    expect(request?.path).toEqual({ scheduleRef: current.ref });
    expect(request?.body).toEqual({ ...input, name: updated.name });
    expect(request?.headers["If-Match"]).toBe('"4"');
    expect(store.schedules[current.ref]).toEqual(updated);
  });

  it("архивирует через специализированную OCC-команду, а не delete path", async () => {
    const current = schedule();
    const archived = schedule({
      version: 5,
      state: "ARCHIVED",
      nextActions: ["OPEN"],
    });
    commandScheduleMock.mockResolvedValue({
      data: archived,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    await store.changeSchedule(current, "ARCHIVE");

    const request = commandScheduleMock.mock.calls[0]?.[0] as
      | Parameters<
          typeof import("@/shared/api/generated/openapi/sdk.gen").commandSchedule
        >[0]
      | undefined;
    expect(request?.path).toEqual({ scheduleRef: current.ref });
    expect(request?.body).toEqual({ action: "ARCHIVE" });
    expect(request?.headers["If-Match"]).toBe('"4"');
    expect(store.schedules[current.ref]).toEqual(archived);
  });

  it("не перезаписывает mutation устаревшим list readback", async () => {
    const current = schedule();
    const paused = schedule({
      version: 5,
      state: "PAUSED",
      nextActions: ["EDIT", "ENABLE", "ARCHIVE"],
    });
    let resolveList:
      | ((value: { data: { items: Schedule[] }; response: Response }) => void)
      | undefined;
    listSchedulesMock.mockReturnValue(
      new Promise((resolve) => {
        resolveList = resolve;
      }),
    );
    commandScheduleMock.mockResolvedValue({
      data: paused,
      response: new Response(null, { status: 200 }),
    });
    const store = usePlatformStore();

    const staleReload = store.loadSchedules(current.projectRef);
    await store.changeSchedule(current, "PAUSE");
    resolveList?.({
      data: { items: [current] },
      response: new Response(null, { status: 200 }),
    });
    await staleReload;

    expect(store.schedules[current.ref]).toEqual(paused);
  });
});
