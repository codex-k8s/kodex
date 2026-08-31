import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AutomationsWorkspace.vue", import.meta.url),
  "utf8",
);

describe("AutomationsWorkspace lifecycle contract", () => {
  it("загружает списки, revisions и runs через server cursor", () => {
    expect(source).toContain("loadSchedulePage(");
    expect(source).toContain("loadScheduleRevisionPage(");
    expect(source).toContain("loadScheduleRunPage(");
    expect(source).toContain("nextPageToken");
    expect(source).toContain("IntersectionObserver");
    expect(source).toContain('v-if="nextPageToken"');
  });

  it("не сопоставляет историю запусков по mutable имени", () => {
    expect(source).toContain("ScheduleRunOccurrence");
    expect(source).toContain("occurrence.scheduleRef");
    expect(source).toContain("occurrence.scheduleRevisionRef");
    expect(source).toContain("occurrence.scheduleRevision");
    expect(source).toContain("/runs/${occurrence.run.ref}");
    expect(source).not.toContain("run.title === selectedSchedule.name");
  });

  it("показывает lifecycle только через nextActions и подтверждает delete", () => {
    expect(source).toContain("selectedCapabilities?.canDelete");
    expect(source).toContain('schedule.nextActions.includes("DELETE")');
    expect(source).toContain("deleteScheduleRef");
    expect(source).toContain("confirmDelete");
  });

  it("использует единый realtime-aware store для lifecycle-команд", () => {
    expect(source).toContain("const scheduleRefs = ref<string[]>([])");
    expect(source).toContain("platform.schedules[ref]");
    expect(source).toContain(
      "replaceSchedule(await commandSchedule(schedule, action))",
    );
    expect(source).not.toContain("platform.changeSchedule(schedule, action)");
    expect(source).not.toContain("const schedules = ref<Schedule[]>([])");
  });

  it("сохраняет выбранную запись после исключения из текущего фильтра", () => {
    expect(source).toContain("[schedules, filteredSchedules]");
    expect(source).toContain(
      "allSchedules.some((schedule) => schedule.ref === selectedRef.value)",
    );
  });

  it("перечитывает authoritative schedule перед OCC lifecycle-командой", () => {
    expect(source).toContain("async function refreshExact");
    expect(source).toContain(
      "const schedule = await refreshExact(scheduleRef)",
    );
    expect(source).toContain(
      "replaceSchedule(await commandSchedule(schedule, action))",
    );
    expect(source).toContain("runCommand(selectedSchedule.ref, 'PAUSE')");
    expect(source).toContain("runCommand(selectedSchedule.ref, 'ENABLE')");
    expect(source).not.toContain("runCommand(selectedSchedule, 'PAUSE')");
  });
});
