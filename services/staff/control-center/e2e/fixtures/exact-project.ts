import type { Page } from "@playwright/test";
import type {
  Project,
  ProjectPage,
} from "../../src/shared/api/generated/openapi/types.gen";

// exactProject читает проект по точному имени и отклоняет неуспешный или неоднозначный ответ.
export async function exactProject(
  page: Page,
  projectName: string,
): Promise<Project> {
  return await page.evaluate(async (name) => {
    const response = await fetch(
      `/api/v1/projects?query=${encodeURIComponent(name)}&pageSize=30`,
    );
    if (!response.ok) throw new Error("Acceptance project read failed");
    const result = (await response.json()) as ProjectPage;
    const matches = result.items.filter((item) => item.name === name);
    if (matches.length !== 1)
      throw new Error("Acceptance project is not unique");
    return matches[0] as Project;
  }, projectName);
}
