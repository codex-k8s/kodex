import type { Page } from "@playwright/test";
import type {
  Agent,
  AgentPage,
} from "../../src/shared/api/generated/openapi/types.gen";

// exactAgent читает одного несистемного сотрудника в точном проекте либо возвращает ошибку.
export async function exactAgent(
  page: Page,
  projectRef: string,
  agentName: string,
  notUnique: string,
): Promise<Agent> {
  return await page.evaluate(
    async ({ name, project, notUnique }) => {
      const query = new URLSearchParams({
        projectRef: project,
        query: name,
        pageSize: "40",
      });
      const response = await fetch(`/api/v1/agents?${query}`);
      if (!response.ok) throw new Error("Agent catalog read failed");
      const result = (await response.json()) as AgentPage;
      const matches = result.items.filter(
        (item) =>
          item.projectRef === project && !item.system && item.name === name,
      );
      if (matches.length !== 1) throw new Error(notUnique);
      return matches[0] as Agent;
    },
    { name: agentName, project: projectRef, notUnique },
  );
}
