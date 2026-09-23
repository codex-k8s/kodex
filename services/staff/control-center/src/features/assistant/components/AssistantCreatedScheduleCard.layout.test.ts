import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantCreatedScheduleCard.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("карточка созданной помощником автоматизации", () => {
  it("связывает состояние и переход с точной квитанцией и readback проекта", () => {
    expect(source).toContain("assistantCreatedScheduleTarget");
    expect(source).toContain("readSchedule(value.scheduleRef");
    expect(source).toContain("next.ref !== value.scheduleRef");
    expect(source).toContain("next.projectRef !== value.projectRef");
    expect(source).toContain("timeZone: current.timezone");
    expect(workspace).toContain("<AssistantCreatedScheduleCard");
    expect(workspace).toContain("item.type === 'CREATE_SCHEDULE'");
  });
});
