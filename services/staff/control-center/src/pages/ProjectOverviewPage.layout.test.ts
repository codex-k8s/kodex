import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./ProjectOverviewPage.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("ProjectOverviewPage layout", () => {
  it("размещает текущую работу слева, а ресурсы в компактном правом rail", () => {
    const main = template.indexOf('class="project-workboard__main"');
    const resources = template.indexOf('class="project-workboard__resources"');

    expect(main).toBeGreaterThan(-1);
    expect(resources).toBeGreaterThan(main);
    expect(template).not.toContain("project-resources-section");
  });

  it("передаёт только фактически загруженные автоматизации и окружения", () => {
    expect(template).toContain(':schedules="schedules"');
    expect(template).toContain(':environments="environments"');
    expect(template).toContain(':schedules-ready="schedulesReady"');
    expect(template).toContain(':environments-ready="environmentsReady"');
  });

  it("показывает загруженных ИИ-сотрудников, а не только счётчик Проекта", () => {
    expect(source).toContain("platform.loadAgents(projectRef.value)");
    expect(template).toContain("<ProjectAgentList");
    expect(template).toContain(':agents="projectAgents.slice(0, 8)"');
    expect(template).toContain(':problem="platform.problems.agents"');
  });
});
