import { describe, expect, it } from "vitest";
import {
  validWorkflowDAG,
  workflowEvidenceProfileSteps,
  validWorkflowRequirements,
  validWorkflowKey,
  workflowModeSteps,
  newWorkflowStepKey,
} from "./result-contract";
import type { WorkflowResultRequirement } from "@/shared/api/generated/openapi/types.gen";

it("сохраняет native файл и комментарий только в явном PDLC и закрывает смену target", () => {
  type PDLCRequirements = [
    WorkflowResultRequirement,
    WorkflowResultRequirement,
    WorkflowResultRequirement,
  ];
  const requirements: PDLCRequirements = [
    {
      key: "document",
      kind: "REPOSITORY_FILE",
      repository: "owner/repo",
      targetRef: "3",
      branch: "feature/journal",
      fileName: "docs/requirements.md",
    },
    {
      key: "comment",
      kind: "PR_COMMENT",
      repository: "owner/repo",
      targetRef: "3",
    },
    {
      key: "report",
      kind: "ARTIFACT",
      fileName: "requirements.md",
      mediaType: "text/markdown",
    },
  ];
  expect(validWorkflowRequirements(requirements, [], "GITHUB_PDLC_V1")).toBe(
    true,
  );
  const steps = [
    {
      ref: "author",
      agentRef: "agt_author01",
      dependsOn: [],
      requiredCapabilityKeys: [],
      resultRequirements: requirements,
    },
  ];
  expect(validWorkflowDAG(steps, 2, "", "ref", "", "GITHUB_PDLC_V1")).toBe(
    true,
  );
  for (const profile of [undefined, "GITHUB_REVIEW_QA_V1", "GITHUB_PDLC_V2"])
    expect(validWorkflowRequirements(requirements, [], profile)).toBe(false);
  for (const mutate of [
    (r: PDLCRequirements) => {
      r[0].branch = "refs/heads/feature/journal";
    },
    (r: PDLCRequirements) => {
      r[0].fileName = "../requirements.md";
    },
    (r: PDLCRequirements) => {
      r[0].targetRef = "03";
    },
    (r: PDLCRequirements) => {
      r[1].targetRef = "4";
    },
    (r: PDLCRequirements) => {
      r[1].repository = "owner/foreign";
    },
    (r: PDLCRequirements) => {
      r[1].branch = "feature/journal";
    },
    (r: PDLCRequirements) => {
      r[1] = { ...r[0], key: "second-file" };
    },
  ]) {
    const changed = structuredClone(requirements);
    mutate(changed);
    expect(validWorkflowRequirements(changed, [], "GITHUB_PDLC_V1")).toBe(
      false,
    );
  }
  const original = structuredClone(steps);
  const changed = workflowEvidenceProfileSteps(steps, undefined);
  const changedStep = changed[0];
  if (!changedStep) throw new Error("PDLC profile copy lost its exact step");
  expect(changedStep.resultRequirements[0].branch).toBe("feature/journal");
  expect(
    validWorkflowRequirements(changedStep.resultRequirements, [], undefined),
  ).toBe(false);
  expect(steps).toEqual(original);
});

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

it("связывает QA с JSON ARTIFACT только явно выбранного профиля", () => {
  const requirements = [
    {
      key: "report",
      kind: "ARTIFACT",
      fileName: "qa.json",
      mediaType: "application/json",
    },
    { key: "qa", kind: "QA", subjectStepKey: "author", evidenceKey: "report" },
  ];
  expect(
    validWorkflowRequirements(requirements, ["author"], "GITHUB_REVIEW_QA_V1"),
  ).toBe(true);
  expect(validWorkflowRequirements(requirements, ["author"])).toBe(false);
  expect(validWorkflowRequirements(requirements, ["author"], "UNKNOWN")).toBe(
    false,
  );
  expect(
    validWorkflowRequirements(
      [{ ...requirements[0], mediaType: "text/plain" }, requirements[1]],
      ["author"],
      "GITHUB_REVIEW_QA_V1",
    ),
  ).toBe(false);
  expect(
    validWorkflowRequirements(
      [requirements[0], { ...requirements[1], evidenceKey: "foreign" }],
      ["author"],
      "GITHUB_REVIEW_QA_V1",
    ),
  ).toBe(false);
});

it("explicit profile clear удаляет только editable evidenceKey без alias исходных requirements", () => {
  const original = [
    {
      key: "qa",
      dependsOn: ["author"],
      resultRequirements: [
        {
          key: "report",
          kind: "ARTIFACT",
          fileName: "qa.json",
          mediaType: "application/json",
        },
        {
          key: "qa",
          kind: "QA",
          subjectStepKey: "author",
          evidenceKey: "report",
        },
      ],
    },
  ];
  const before = structuredClone(original);
  const cleared = workflowEvidenceProfileSteps(original, undefined);
  expect(cleared[0]?.resultRequirements[1]).not.toHaveProperty("evidenceKey");
  expect(original).toEqual(before);
  expect(cleared[0]?.dependsOn).toEqual(["author"]);
});

it("новый evidence профиль требует direct predecessor с единственным PULL_REQUEST", () => {
  const steps = graph();
  steps.push({
    ref: "checker",
    agentRef: "checker",
    dependsOn: ["review"],
    requiredCapabilityKeys: [],
    resultRequirements: [
      { key: "checked", kind: "REVIEW", subjectStepKey: "review" },
    ],
  });
  expect(validWorkflowDAG(steps, 2, "", "ref")).toBe(true);
  expect(validWorkflowDAG(steps, 2, "", "ref", "", "GITHUB_REVIEW_QA_V1")).toBe(
    false,
  );
  at(steps, 3).dependsOn = ["code"];
  at(steps, 3).resultRequirements = [
    { key: "checked", kind: "REVIEW", subjectStepKey: "code" },
  ];
  expect(validWorkflowDAG(steps, 2, "", "ref", "", "GITHUB_REVIEW_QA_V1")).toBe(
    true,
  );
  at(steps, 1).resultRequirements = [
    { key: "pr", kind: "PULL_REQUEST", repository: "codex-k8s/test-repo" },
    {
      key: "other_pr",
      kind: "PULL_REQUEST",
      repository: "codex-k8s/test-repo",
    },
  ];
  expect(validWorkflowDAG(steps, 2, "", "ref", "", "GITHUB_REVIEW_QA_V1")).toBe(
    false,
  );
});

it("разрешает проверку exact опубликованного файла только с явной политикой", () => {
  const steps = [
    {
      ref: "author",
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
      ref: "review",
      agentRef: "reviewer",
      dependsOn: ["author"],
      requiredCapabilityKeys: [],
      resultRequirements: [
        {
          key: "verdict",
          kind: "ARTIFACT_REVIEW",
          subjectStepKey: "author",
          subjectResultKey: "spec",
        },
      ],
    },
  ];
  expect(
    validWorkflowDAG(steps, 2, "", "ref", "OWNER_APPROVED_UPSTREAM_V1"),
  ).toBe(true);
  expect(validWorkflowDAG(steps, 2, "", "ref")).toBe(false);
  const missing = structuredClone(steps);
  const missingRequirement = missing.at(1)?.resultRequirements.at(0);
  if (!missingRequirement || !("subjectResultKey" in missingRequirement))
    throw new Error("fixture requirement missing");
  missingRequirement.subjectResultKey = "foreign";
  expect(
    validWorkflowDAG(missing, 2, "", "ref", "OWNER_APPROVED_UPSTREAM_V1"),
  ).toBe(false);
  const self = structuredClone(steps);
  const selfReviewer = self.at(1);
  if (!selfReviewer) throw new Error("fixture reviewer missing");
  selfReviewer.agentRef = "analyst";
  expect(
    validWorkflowDAG(self, 2, "", "ref", "OWNER_APPROVED_UPSTREAM_V1"),
  ).toBe(false);
});
