import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantLaunchedRunCard.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("карточка запущенного помощником сотрудника или процесса", () => {
  it("связывает запуск с точной квитанцией и авторитетным readback", () => {
    expect(source).toContain("assistantLaunchedRunTarget");
    expect(source).toContain("current.projectRef !== projectRef");
    expect(source).toContain('current.source !== "SYSTEM_ASSISTANT"');
    expect(workspace).toContain("<AssistantLaunchedRunCard");
    expect(workspace).toContain("item.type === 'LAUNCH_RUN'");
  });

  it("показывает состояние и останавливает только разрешённый запуск", () => {
    expect(source).toContain('run.value?.nextActions.includes("CANCEL")');
    expect(source).toContain("run?.nextActions.includes('CANCEL')");
    expect(source).toContain('current.nextActions.includes("CANCEL")');
    expect(source).toContain(
      'platform.changeRun(current, { action: "CANCEL" })',
    );
    expect(source).toContain("run.state === 'WAITING_HUMAN'");
    expect(source).toContain("run.safeErrorCode");
    expect(source).toContain("run.resultSummary");
  });
});
