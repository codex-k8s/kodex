import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const panel = readFileSync(
  new URL("./BindingsPanel.vue", import.meta.url),
  "utf8",
);
const page = readFileSync(
  new URL("../../../pages/AccessPage.vue", import.meta.url),
  "utf8",
);
const participants = readFileSync(
  new URL("./ParticipantsPanel.vue", import.meta.url),
  "utf8",
);

describe("BindingsPanel list contract", () => {
  it("передаёт серверу поиск, состояние и адаптивный размер cursor-страницы", () => {
    expect(panel).toContain('name="access-binding-search"');
    expect(panel).toContain("useAdaptiveCursorPageSize");
    expect(panel).toContain("useCursorInfiniteScroll");
    expect(panel).toContain('stateFilter.value === "ALL"');
    expect(panel).toContain('emit("search"');
    expect(panel).toContain('"more",');
    expect(panel).toContain('itemSelector: ".binding-row"');
    expect(panel).toContain('class="binding-table"');
    expect(panel).not.toContain('class="binding-card"');
    expect(page).toContain("query, includeRevoked, pageSize");
    expect(page).toContain("access.loadBindings(");
  });

  it("не позволяет менять служебные проекции членства как прямые назначения", () => {
    expect(panel).toContain('binding.managementKind === "DIRECT"');
    expect(panel).toContain('v-if="!isDirect(binding)"');
    expect(panel).toContain("emit('manage-membership', binding)");
    expect(page).toContain('@manage-membership="manageBindingMembership"');
    expect(page).toContain("memberRef: binding.subject.ref");
    expect(participants).toContain("bindingPresentationKey(binding)");
    expect(participants).toContain(
      'binding.managementKind === "PROJECT_MEMBERSHIP"',
    );
  });
});
