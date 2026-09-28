import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const panel = readFileSync(
  new URL("./ParticipantsPanel.vue", import.meta.url),
  "utf8",
);

describe("ParticipantsPanel access presentation", () => {
  it("показывает состояние доступа из активного платформенного членства", () => {
    expect(panel).toContain("function participantActive");
    expect(panel).toContain('subject.kind !== "USER"');
    expect(panel).toContain("platformMembership(subject)?.active === true");
    expect(panel).toContain(":state=\"participantActive(subject)");
    expect(panel).toContain("!participantActive(selectedSubject)");
  });

  it("локализует действия участника", () => {
    expect(panel).toContain("access.participants.inspectEffectiveFor");
    expect(panel).toContain("access.participants.editPlatformRole");
    expect(panel).toContain("access.participants.removeFromOrganization");
  });
});
