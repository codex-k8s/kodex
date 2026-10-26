import type { WorkflowResultRequirement } from "@/shared/api/generated/openapi/types.gen";

export const workflowResultKinds = [
  "ARTIFACT",
  "PULL_REQUEST",
  "REVIEW",
  "QA",
  "INTEGRATION_RECEIPT",
  "DEPLOYMENT",
] as const;
export type WorkflowExecutionMode = "MANAGER_DRIVEN" | "SERVER_DAG_V1";
export type WorkflowResultContract = {
  ref?: string;
  dependsOn?: string[];
  resultRequirements?: WorkflowResultRequirement[];
};
type Item = Record<string, unknown>;
const encoder = new TextEncoder();
const byteLength = (value: string) => encoder.encode(value).length;
export function workflowItem(value: unknown): value is Item {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function validWorkflowKey(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length >= 1 &&
    value.length <= 96 &&
    !/[^A-Za-z0-9_-]/u.test(value)
  );
}
function boundedText(value: unknown, max: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    byteLength(value) <= max &&
    !["\0", "\r", "\n"].some((character) => value.includes(character))
  );
}

// Проверка формы до отправки; полномочия и доказательства проверяет сервер.
export function validWorkflowRequirements(
  value: unknown,
  dependencies: string[],
): value is WorkflowResultRequirement[] {
  if (!Array.isArray(value) || value.length < 1 || value.length > 4)
    return false;
  const seen = new Set<string>();
  for (const item of value) {
    if (
      !workflowItem(item) ||
      !validWorkflowKey(item.key) ||
      seen.has(item.key)
    )
      return false;
    seen.add(item.key);
    const keys = ["key", "kind"];
    switch (item.kind) {
      case "ARTIFACT":
        keys.push("fileName", "mediaType");
        if (
          !boundedText(item.fileName, 160) ||
          /[/\\]/u.test(item.fileName) ||
          [".", "..", "result.md", "workspace-write-result.json"].includes(
            item.fileName,
          ) ||
          !boundedText(item.mediaType, 160)
        )
          return false;
        break;
      case "PULL_REQUEST":
        keys.push("repository");
        break;
      case "REVIEW":
      case "QA":
        keys.push("subjectStepKey");
        break;
      case "INTEGRATION_RECEIPT":
        keys.push("repository", "subjectStepKey", "operation");
        if (item.operation !== "github.pull_request.merge") return false;
        break;
      case "DEPLOYMENT":
        keys.push("repository", "subjectStepKey", "operation", "targetRef");
        if (
          item.operation !== "ssh.compose.deploy" ||
          !boundedText(item.targetRef, 96) ||
          !item.targetRef.startsWith("ssh_")
        )
          return false;
        break;
      default:
        return false;
    }
    if (
      Object.keys(item).some((key) => !keys.includes(key)) ||
      keys.some((key) => typeof item[key] !== "string" || item[key] === "")
    )
      return false;
    if (
      keys.includes("repository") &&
      (!boundedText(item.repository, 200) ||
        /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/u.exec(item.repository)?.[0] !==
          item.repository)
    )
      return false;
    if (
      keys.includes("subjectStepKey") &&
      (typeof item.subjectStepKey !== "string" ||
        !dependencies.includes(item.subjectStepKey))
    )
      return false;
  }
  return true;
}

export function validWorkflowDAG(
  steps: unknown,
  concurrency: unknown,
  text: string,
  keyField: "ref" | "key",
): boolean {
  if (
    !Array.isArray(steps) ||
    steps.length < 1 ||
    steps.length > 12 ||
    typeof concurrency !== "number" ||
    !Number.isInteger(concurrency) ||
    concurrency < 1 ||
    concurrency > 2 ||
    byteLength(text) > 8192
  )
    return false;
  const agents = new Map<string, string>();
  const reviewers = new Map<string, string>();
  for (const step of steps) {
    if (!workflowItem(step)) return false;
    const key = step[keyField];
    const deps: unknown = step.dependsOn;
    if (
      !validWorkflowKey(key) ||
      agents.has(key) ||
      typeof step.agentRef !== "string" ||
      !step.agentRef ||
      !Array.isArray(deps) ||
      deps.length > 8 ||
      new Set(deps).size !== deps.length ||
      !deps.every(
        (dep): dep is string => typeof dep === "string" && agents.has(dep),
      ) ||
      !validWorkflowRequirements(step.resultRequirements, deps)
    )
      return false;
    if (
      !Array.isArray(step.requiredCapabilityKeys) ||
      step.requiredCapabilityKeys.some(
        (cap) =>
          cap === "platform.run.delegate" || cap === "platform.run.launch",
      )
    )
      return false;
    for (const requirement of step.resultRequirements) {
      if (requirement.kind !== "REVIEW") continue;
      const subject = requirement.subjectStepKey ?? "";
      const reviewer = `${subject}:${step.agentRef}`;
      if (
        agents.get(subject) === step.agentRef ||
        (reviewers.has(reviewer) && reviewers.get(reviewer) !== key)
      )
        return false;
      reviewers.set(reviewer, key);
    }
    agents.set(key, step.agentRef);
  }
  return true;
}

export function newWorkflowStepKey(keys: unknown[]): string {
  let index = 1;
  while (keys.includes(`step-${String(index)}`)) index++;
  return `step-${String(index)}`;
}

// Переключение режима не выводит зависимости по порядку и не удаляет старые поля молча:
// вызывающая форма предварительно показывает подтверждение изменения режима.
export function workflowModeSteps<T extends object>(
  steps: T[],
  mode: WorkflowExecutionMode,
  keyField: "ref" | "key",
  keepLegacyKey: boolean,
): T[] {
  const keys = steps.map((step) => (step as Item)[keyField]);
  return steps.map((step) => {
    const next = { ...step } as Item;
    if (mode === "SERVER_DAG_V1") {
      if (!next[keyField]) {
        next[keyField] = newWorkflowStepKey(keys);
        keys.push(next[keyField]);
      }
      next.dependsOn ??= [];
      next.resultRequirements ??= [];
    } else {
      delete next.dependsOn;
      delete next.resultRequirements;
      if (!keepLegacyKey) {
        if (keyField === "ref") delete next.ref;
        else delete next.key;
      }
    }
    return next as T;
  });
}
