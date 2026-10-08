import { describe, expect, it } from "vitest";
import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";
import {
  assertAssistantRetryIdentity,
  assertRunOwner,
  sameAssistantRunPin,
  runNodeExecutionLabels,
  runNodePresentationKey,
  runSessionStorageBlocker,
} from "./run-owner";
function assistantRun(scope: "SYSTEM" | "PROJECT" = "SYSTEM"): Run {
  return {
    ref: "run_fixture",
    sessionRef: "ses_fixture",
    attempt: 1,
    source: "SYSTEM_ASSISTANT",
    target: {
      type: "SYSTEM_ASSISTANT",
      ref: "agt_fixture",
      displayName: "Kodex",
      version: 1,
    },
    ...(scope === "PROJECT" ? { projectRef: "prj_fixture" } : {}),
    assistantPin: {
      scope,
      organizationRef: "org_fixture",
      conversationRef: "cnv_fixture",
      assistantRef: "agt_fixture",
      ...(scope === "PROJECT"
        ? { projectRef: "prj_fixture", profileRef: "asstp_fixture" }
        : {}),
    },
  } as Run;
}
function assistantPin(run: Run) {
  if (!run.assistantPin) throw new Error("Missing synthetic assistant pin");
  return run.assistantPin;
}
describe("closed owner pin запуска помощника", () => {
  it.each(["ERROR", "PURGED"] as const)(
    "возвращает только exact session blocker %s без вывода о готовности",
    (storageState) => {
      const value = assistantRun();
      value.sessionReadiness = {
        sessionRef: value.sessionRef,
        storageState,
        reason: "STORAGE_NOT_LIVE",
      };
      expect(runSessionStorageBlocker(value, "org_fixture")).toBe(storageState);
      expect(runSessionStorageBlocker(value, "org_foreign")).toBeUndefined();
      expect(
        runSessionStorageBlocker(undefined, "org_fixture"),
      ).toBeUndefined();
      expect(
        runSessionStorageBlocker(
          { ...value, sessionReadiness: undefined },
          "org_fixture",
        ),
      ).toBeUndefined();
      expect(
        runSessionStorageBlocker(
          {
            ...value,
            sessionReadiness: {
              ...value.sessionReadiness,
              sessionRef: "ses_other",
            },
          },
          "org_fixture",
        ),
      ).toBeUndefined();
      for (const state of [
        "LIVE",
        "UNTRACKED",
        "RESTORING",
        "ARCHIVED",
      ] as const)
        expect(
          runSessionStorageBlocker(
            {
              ...value,
              sessionReadiness: {
                ...value.sessionReadiness,
                storageState: state,
              },
            },
            "org_fixture",
          ),
        ).toBeUndefined();
    },
  );
  it.each(["SYSTEM", "PROJECT"] as const)(
    "подписывает доказанные %s ходы, но не delegated employee",
    (scope) => {
      const assistant = assistantRun(scope);
      const employee: Run = {
        ...assistantRun("PROJECT"),
        ref: "run_employee",
        source: "AGENT_DELEGATION",
        target: {
          type: "AGENT",
          ref: "agt_employee",
          displayName: "Сотрудник",
          version: 1,
        },
        assistantPin: undefined,
      };
      const node = (ref: string, runRef: string, agentRef: string) =>
        ({
          ref,
          runRef,
          agentRef,
          type: "AGENT_EXECUTION",
        }) as RunNode;
      const labels = runNodeExecutionLabels(
        [
          node("node_assistant", assistant.ref, "agt_fixture"),
          node("node_employee", employee.ref, "agt_employee"),
          node("node_unknown", "run_unknown", "agt_fixture"),
          node("node_mixed", assistant.ref, "agt_employee"),
        ],
        { [assistant.ref]: assistant, [employee.ref]: employee },
        "org_fixture",
      );
      expect(labels).toEqual({
        node_assistant: "ASSISTANT",
        node_employee: "EMPLOYEE",
        node_unknown: "SESSION",
        node_mixed: "SESSION",
      });
      expect(
        runNodeExecutionLabels(
          [node("node_assistant", assistant.ref, "agt_fixture")],
          { [assistant.ref]: assistant },
          "org_foreign",
        ),
      ).toEqual({ node_assistant: "SESSION" });
      expect(runNodePresentationKey("ASSISTANT", "AGENT_EXECUTION")).toBe(
        "runs.assistantNode",
      );
      expect(runNodePresentationKey("SESSION", "AGENT_EXECUTION")).toBe(
        "runs.sessionNode",
      );
      expect(runNodePresentationKey("EMPLOYEE", "AGENT_EXECUTION")).toBe(
        "runs.nodeTypes.AGENT_EXECUTION",
      );
    },
  );
  it.each(["SYSTEM", "PROJECT"] as const)(
    "принимает exact %s и отклоняет missing/foreign anchor",
    (scope) => {
      const run = assistantRun(scope);
      expect(() => assertRunOwner(run, "org_fixture")).not.toThrow();
      expect(() => assertRunOwner(run, undefined)).toThrow("anchor");
      expect(() => assertRunOwner(run, "org_foreign")).toThrow("pin");
    },
  );
  it("SYSTEM с контекстом проекта не становится PROJECT profile", () => {
    const run = assistantRun();
    expect(() =>
      assertRunOwner(
        {
          ...run,
          projectRef: "prj_fixture",
          assistantPin: { ...assistantPin(run), projectRef: "prj_fixture" },
        },
        "org_fixture",
      ),
    ).not.toThrow();
    expect(() =>
      assertRunOwner(
        {
          ...run,
          assistantPin: { ...assistantPin(run), profileRef: "asstp_fixture" },
        },
        "org_fixture",
      ),
    ).toThrow("forbidden");
    expect(() =>
      assertRunOwner({ ...run, projectRef: "" }, "org_fixture"),
    ).toThrow("pin");
  });
  it("обычный запуск требует проект и запрещает assistant pin", () => {
    const run = {
      ...assistantRun(),
      source: "CONTROL_CENTER",
      target: {
        type: "AGENT",
        ref: "agt_fixture",
        displayName: "Агент",
        version: 1,
      },
    } as Run;
    expect(() => assertRunOwner(run, "org_fixture")).toThrow("Regular");
    expect(() =>
      assertRunOwner(
        { ...run, projectRef: "prj_fixture", assistantPin: undefined },
        undefined,
      ),
    ).not.toThrow();
  });
  it("retry сохраняет все поля pin и session, новая попытка отдельная", () => {
    const old = assistantRun("PROJECT"),
      next = {
        ...old,
        ref: "run_retry_fixture",
        attempt: 2,
        retryOfRunRef: old.ref,
      };
    expect(() => assertAssistantRetryIdentity(old, next)).not.toThrow();
    for (const key of [
      "organizationRef",
      "conversationRef",
      "assistantRef",
      "profileRef",
      "projectRef",
    ] as const)
      expect(() =>
        assertAssistantRetryIdentity(old, {
          ...next,
          assistantPin: { ...assistantPin(next), [key]: "ref_foreign" },
        }),
      ).toThrow("mismatch");
    expect(() =>
      assertAssistantRetryIdentity(old, { ...next, sessionRef: "ses_foreign" }),
    ).toThrow("mismatch");
    expect(
      sameAssistantRunPin(old, {
        ...next,
        assistantPin: {
          assistantRef: "agt_fixture",
          profileRef: "asstp_fixture",
          projectRef: "prj_fixture",
          conversationRef: "cnv_fixture",
          organizationRef: "org_fixture",
          scope: "PROJECT",
        },
      }),
    ).toBe(true);
  });
});
