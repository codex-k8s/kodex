import { describe, expect, it } from "vitest";

import {
  isAssistantSetupRequest,
  isAssistantRoleImageBuildDebugRequest,
  isAssistantRunDebugRequest,
} from "@/features/assistant/events";

describe("запрос помощника для первичной настройки", () => {
  it.each(["project", "image", "environment", "team", "launch"])(
    "принимает известный шаг %s",
    (step) => {
      expect(isAssistantSetupRequest({ kind: "SETUP", step })).toBe(true);
    },
  );
  it.each([
    undefined,
    {},
    { kind: "SETUP", step: "model" },
    { kind: "SETUP", step: "../project" },
    { kind: "SETUP", step: 1 },
  ])("не принимает неизвестный запрос %j", (value) => {
    expect(isAssistantSetupRequest(value)).toBe(false);
  });
});

const valid = {
  kind: "ROLE_IMAGE_BUILD_DEBUG",
  recipeRef: "imgrec_example01",
  buildRef: "imgbld_example01",
  attempt: 3,
  stage: "DEAD_LETTER",
  safeErrorCode: "INSTALLATION_FAILED",
  diagnosticCode: "INSTALL_COMMAND_REJECTED",
  diagnosticSummary: "Installation step failed",
};

describe("запрос диагностики сборки в помощнике", () => {
  it("принимает точную безопасную terminal-попытку", () => {
    expect(isAssistantRoleImageBuildDebugRequest(valid)).toBe(true);
  });

  it.each([
    { recipeRef: "../wrong" },
    { buildRef: "../wrong" },
    { attempt: 0 },
    { attempt: 1.5 },
    { stage: "COMPLETED" },
    { safeErrorCode: "unsafe\ncode" },
    { safeErrorCode: 123 },
    { diagnosticCode: 123 },
    { diagnosticSummary: "x".repeat(257) },
  ])("отклоняет повреждённые поля %j", (change) => {
    expect(isAssistantRoleImageBuildDebugRequest({ ...valid, ...change })).toBe(
      false,
    );
  });
});

const validRun = {
  kind: "RUN_DEBUG",
  runRef: "run_example01",
  rootRunRef: "run_root01",
  targetType: "WORKFLOW",
  attempt: 2,
  safeErrorCode: "PROVIDER_UNAVAILABLE",
  failedNodes: [
    {
      nodeRef: "nod_example01",
      type: "AGENT_EXECUTION",
      agentRef: "agt_example01",
      safeErrorCode: "PROVIDER_UNAVAILABLE",
    },
  ],
};

describe("запрос диагностики запуска в помощнике", () => {
  it("принимает безопасный terminal-срез запуска", () => {
    expect(isAssistantRunDebugRequest(validRun)).toBe(true);
  });

  it.each([
    { runRef: "../wrong" },
    { rootRunRef: "../wrong" },
    { targetType: "PROJECT" },
    { attempt: 0 },
    { attempt: 1.5 },
    { safeErrorCode: "unsafe\ncode" },
    { failedNodes: [] },
    { failedNodes: Array.from({ length: 21 }, () => validRun.failedNodes[0]) },
    { failedNodes: [{ ...validRun.failedNodes[0], type: "WRONG" }] },
    { failedNodes: [{ ...validRun.failedNodes[0], agentRef: "../wrong" }] },
  ])("отклоняет повреждённые поля %j", (change) => {
    expect(isAssistantRunDebugRequest({ ...validRun, ...change })).toBe(false);
  });
});
