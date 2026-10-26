import { describe, expect, it } from "vitest";
import {
  validWorkflowDAG,
  validWorkflowRequirements,
  validWorkflowKey,
  workflowModeSteps,
  newWorkflowStepKey,
} from "./result-contract";
import type { WorkflowResultRequirement } from "@/shared/api/generated/openapi/types.gen";

function graph() {
  return [
    {
      ref: "spec",
      agentRef: "analyst",
      dependsOn: [],
      requiredCapabilityKeys: [],
      resultRequirements: [
        {
          key: "spec",
          kind: "ARTIFACT",
          fileName: "spec.md",
          mediaType: "text/markdown",
        },
      ],
    },
    {
      ref: "code",
      agentRef: "developer",
      dependsOn: ["spec"],
      requiredCapabilityKeys: [],
      resultRequirements: [
        { key: "pr", kind: "PULL_REQUEST", repository: "codex-k8s/test-repo" },
      ],
    },
    {
      ref: "review",
      agentRef: "reviewer",
      dependsOn: ["code"],
      requiredCapabilityKeys: [],
      resultRequirements: [
        { key: "review", kind: "REVIEW", subjectStepKey: "code" },
      ],
    },
  ];
}
function at(steps: ReturnType<typeof graph>, index: number) {
  const step = steps[index];
  if (!step) throw new Error("Workflow fixture step missing");
  return step;
}
describe("условия автоматического процесса", () => {
  it.each(["\n", "\r", "\u2028", "\u2029"])(
    "не принимает конечный перевод строки как часть ключа или репозитория %j",
    (suffix) => {
      expect(validWorkflowKey(`code${suffix}`)).toBe(false);
      expect(
        validWorkflowRequirements(
          [
            {
              key: "pr",
              kind: "PULL_REQUEST",
              repository: `codex-k8s/test-repo${suffix}`,
            },
          ],
          [],
        ),
      ).toBe(false);
    },
  );
  it("проверяет порядок, независимость и полный текст, сохраняя допустимую конфигурацию", () => {
    const steps = graph();
    expect(validWorkflowDAG(steps, 2, "я".repeat(4096), "ref")).toBe(true);
    expect(validWorkflowDAG(steps, 2, "я".repeat(4097), "ref")).toBe(false);
    expect(validWorkflowDAG(steps, 3, "", "ref")).toBe(false);
    expect(validWorkflowDAG(steps, 1.5, "", "ref")).toBe(false);
    expect(validWorkflowDAG([...steps].reverse(), 2, "", "ref")).toBe(false);
    expect(
      validWorkflowDAG(
        [...steps, { ...steps[2], ref: "security" }],
        2,
        "",
        "ref",
      ),
    ).toBe(false);
  });
  it.each([
    (steps: ReturnType<typeof graph>) => {
      at(steps, 2).agentRef = "developer";
    },
    (steps: ReturnType<typeof graph>) => {
      at(steps, 1).dependsOn = ["spec", "spec"];
    },
    (steps: ReturnType<typeof graph>) => {
      at(steps, 1).dependsOn = ["missing"];
    },
    (steps: ReturnType<typeof graph>) => {
      at(steps, 1).ref = "spec";
    },
    (steps: ReturnType<typeof graph>) => {
      at(steps, 1).ref = "code\n";
    },
    (steps: ReturnType<typeof graph>) => {
      at(steps, 2).resultRequirements = [];
    },
  ])(
    "закрывает сохранение повреждённой зависимости или результата",
    (mutate) => {
      const steps = graph();
      mutate(steps);
      expect(validWorkflowDAG(steps, 2, "", "ref")).toBe(false);
    },
  );
  it("запрещает скрытые полномочия и лишние поля условий", () => {
    const steps = graph();
    expect(
      validWorkflowDAG(
        steps.map((step) => ({
          ...step,
          requiredCapabilityKeys: ["platform.run.delegate"],
        })),
        2,
        "",
        "ref",
      ),
    ).toBe(false);
    expect(
      validWorkflowRequirements(
        [
          {
            key: "pr",
            kind: "PULL_REQUEST",
            repository: "codex-k8s/test-repo",
            actorRef: "owner",
          },
        ],
        [],
      ),
    ).toBe(false);
  });
  it.each<WorkflowResultRequirement>([
    {
      key: "file",
      kind: "ARTIFACT",
      fileName: "report.md",
      mediaType: "text/markdown",
    },
    { key: "pr", kind: "PULL_REQUEST", repository: "codex-k8s/test-repo" },
    { key: "review", kind: "REVIEW", subjectStepKey: "code" },
    { key: "qa", kind: "QA", subjectStepKey: "code" },
    {
      key: "merge",
      kind: "INTEGRATION_RECEIPT",
      subjectStepKey: "code",
      repository: "codex-k8s/test-repo",
      operation: "github.pull_request.merge",
    },
    {
      key: "deploy",
      kind: "DEPLOYMENT",
      subjectStepKey: "code",
      repository: "codex-k8s/test-repo",
      operation: "ssh.compose.deploy",
      targetRef: "ssh_target",
    },
  ])("принимает закрытый вид $kind", (item) => {
    expect(validWorkflowRequirements([item], ["code"])).toBe(true);
    expect(validWorkflowRequirements([item, item], ["code"])).toBe(false);
  });
  it.each([
    "result.md",
    "workspace-write-result.json",
    "../report.md",
    "report\n.md",
    "я".repeat(81),
  ])(
    "не выдаёт служебный или недопустимый файл за результат: %s",
    (fileName) => {
      expect(
        validWorkflowRequirements(
          [
            {
              key: "file",
              kind: "ARTIFACT",
              fileName,
              mediaType: "text/markdown",
            },
          ],
          [],
        ),
      ).toBe(false);
    },
  );
  it("создаёт уникальные ключи, но не выдумывает рёбра и не меняет исходный черновик", () => {
    const source = [
      { key: "step-1", humanGate: true, approvalTimeoutSeconds: 90 },
      { name: "Второй этап" },
    ];
    const before = structuredClone(source);
    const server = workflowModeSteps(source, "SERVER_DAG_V1", "key", true);
    expect(server[1]).toMatchObject({
      key: "step-2",
      dependsOn: [],
      resultRequirements: [],
    });
    expect(source).toEqual(before);
    expect(workflowModeSteps(server, "MANAGER_DRIVEN", "key", true)[0]).toEqual(
      before[0],
    );
    expect(
      workflowModeSteps(server, "MANAGER_DRIVEN", "key", false)[0],
    ).not.toHaveProperty("key");
    expect(newWorkflowStepKey(["step-1", "step-3"])).toBe("step-2");
  });
});
