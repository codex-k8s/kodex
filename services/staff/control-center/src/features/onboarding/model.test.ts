import { describe, expect, it } from "vitest";
import type {
  Agent,
  Project,
  ProviderAccount,
  RoleImageRecipe,
  Run,
  RuntimeEnvironmentSet,
  SystemAssistant,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import {
  onboardingNavigation,
  onboardingProgress,
  onboardingStep,
} from "./model";

const project = { ref: "prj_first", lifecycle: "ACTIVE" } as Project;
const account = {
  enabled: true,
  ready: true,
  state: "AUTHORIZED",
} as ProviderAccount;
const agent = {
  ref: "agt_worker",
  projectRef: project.ref,
  system: false,
  enabled: true,
  runtimeReady: true,
  state: "READY",
} as Agent;
const assistant = {
  runtimeState: "READY",
  nextActions: ["CREATE_CONVERSATION"],
} as SystemAssistant;
const input = () => ({
  providerAccounts: [account],
  assistant,
  project,
  images: [
    {
      projectRef: project.ref,
      state: "ACTIVE",
      promotedImageReady: true,
    } as RoleImageRecipe,
  ],
  environments: [
    {
      projectRef: project.ref,
      state: "ACTIVE",
      ready: true,
    } as RuntimeEnvironmentSet,
  ],
  agents: [agent],
  workflows: [] as Workflow[],
  runs: [
    {
      projectRef: project.ref,
      state: "SUCCEEDED",
      target: { type: "AGENT", ref: agent.ref },
    } as Run,
  ],
});

describe("пошаговая первичная настройка", () => {
  it("восстанавливает этап и проект, но отбрасывает некорректное сохранённое состояние", () => {
    expect(
      onboardingNavigation({ step: "environment", projectRef: "prj_first" }),
    ).toEqual({ step: "environment", projectRef: "prj_first" });
    for (const value of [
      null,
      "team",
      {},
      { step: "unknown", projectRef: "../../project" },
    ]) {
      expect(onboardingNavigation(value)).toEqual({
        step: "model",
        projectRef: "",
      });
    }
  });
  it("показывает фактическую готовность всех шести шагов", () => {
    const result = onboardingProgress(input());
    expect(result.completedCount).toBe(6);
    expect(result.assistantReady).toBe(true);
  });
  it("не называет помощника готовым без аккаунта даже при старом READY", () => {
    const result = onboardingProgress({ ...input(), providerAccounts: [] });
    expect(result.modelReady).toBe(false);
    expect(result.assistantReady).toBe(false);
  });
  it.each([
    "PENDING_AUTHORIZATION",
    "REAUTHORIZATION_REQUIRED",
    "DISABLED",
    "REVOKED",
  ] as const)("не считает подключённым аккаунт %s", (state) => {
    expect(
      onboardingProgress({
        ...input(),
        providerAccounts: [{ ...account, state }],
      }).modelReady,
    ).toBe(false);
  });
  it("не считает готовым отключённый или непроверенный аккаунт", () => {
    for (const change of [{ enabled: false }, { ready: false }])
      expect(
        onboardingProgress({
          ...input(),
          providerAccounts: [{ ...account, ...change }],
        }).modelReady,
      ).toBe(false);
  });
  it("учитывает отказ помощника и отсутствие CREATE_CONVERSATION", () => {
    for (const change of [
      { runtimeState: "FAILED" as const },
      { nextActions: [] },
    ])
      expect(
        onboardingProgress({
          ...input(),
          assistant: { ...assistant, ...change },
        }).assistantReady,
      ).toBe(false);
  });
  it("не переносит галочки из другого проекта", () => {
    const result = onboardingProgress({
      ...input(),
      project: { ...project, ref: "prj_other" },
    });
    expect(result.completedCount).toBe(2);
    expect(result.complete.team).toBe(false);
    expect(result.complete.launch).toBe(false);
  });
  it("не считает настройку проекта выполненной по архивному или отсутствующему проекту", () => {
    for (const selected of [
      undefined,
      { ...project, lifecycle: "ARCHIVED" as const },
    ])
      expect(
        onboardingProgress({ ...input(), project: selected }).complete.project,
      ).toBe(false);
  });
  it("не засчитывает системного помощника как первого сотрудника и запуск", () => {
    const result = onboardingProgress({
      ...input(),
      agents: [{ ...agent, system: true }],
    });
    expect(result.complete.team).toBe(false);
    expect(result.complete.launch).toBe(false);
  });
  it("засчитывает успешный запуск процесса выбранного проекта", () => {
    const workflow = { ref: "wf_first", projectRef: project.ref } as Workflow;
    expect(
      onboardingProgress({
        ...input(),
        workflows: [workflow],
        runs: [
          {
            ...input().runs[0],
            projectRef: project.ref,
            state: "SUCCEEDED",
            target: {
              type: "WORKFLOW",
              ref: workflow.ref,
              displayName: "",
              version: 1,
            },
          } as Run,
        ],
      }).complete.launch,
    ).toBe(true);
  });
  it.each(["FAILED", "RUNNING", "CANCELLED"] as const)(
    "не засчитывает запуск %s как успешный",
    (state) => {
      expect(
        onboardingProgress({
          ...input(),
          runs: [{ ...input().runs[0], projectRef: project.ref, state } as Run],
        }).complete.launch,
      ).toBe(false);
    },
  );
  it("разбирает только известные шаги", () => {
    expect(onboardingStep("environment")).toBe("environment");
    expect(onboardingStep("unknown")).toBe("model");
    expect(onboardingStep(["team"])).toBe("model");
  });
});
