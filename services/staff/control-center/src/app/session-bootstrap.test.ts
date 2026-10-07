import { createPinia, setActivePinia } from "pinia";
import { reactive } from "vue";
import { beforeEach, describe, expect, it } from "vitest";
import { usePlatformStore } from "@/features/platform/store";
import type {
  BootstrapState,
  OwnerGate,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import { synchronizeSessionBootstrap } from "./session-bootstrap";

function authenticatedBootstrap(): BootstrapState {
  return {
    organizationRef: "org_fixture",
    initialized: true,
    onboardingComplete: true,
    webOnlyReady: true,
    platformRole: "OWNER",
    currentUser: { ref: "user_fixture", displayName: "Владелец" },
    nextActions: [],
    speechTranscription: { available: false, reason: "STT_NOT_CONFIGURED" },
    assistant: { ref: "agt_fixture" } as BootstrapState["assistant"],
  };
}

function systemRun(organizationRef = "org_fixture"): Run {
  return {
    ref: "run_fixture",
    sessionRef: "ses_fixture",
    source: "SYSTEM_ASSISTANT",
    version: 1,
    state: "RUNNING",
    target: {
      type: "SYSTEM_ASSISTANT",
      ref: "agt_fixture",
      displayName: "Kodex",
      version: 1,
    },
    assistantPin: {
      scope: "SYSTEM",
      organizationRef,
      conversationRef: "cnv_fixture",
      assistantRef: "agt_fixture",
    },
  } as Run;
}

function organizationGate(organizationRef = "org_fixture"): OwnerGate {
  return {
    ref: "gate_fixture",
    version: 1,
    scopeKind: "ORGANIZATION",
    organizationRef,
    runRef: "run_fixture",
    nodeRef: "node_fixture",
    title: "Подтвердите действие",
    contextSummary: "Системный помощник",
    consequencesSummary: "Разрешённый эффект",
    requestedBy: { ref: "user_fixture", displayName: "Владелец" },
    state: "OPEN",
    allowedDecisions: ["APPROVE"],
    openedAt: "2026-10-04T00:00:00Z",
    decisionConsequences: [],
    nextActions: [],
  };
}

function snapshot(run = systemRun(), gate = organizationGate()) {
  return { catalog: { runs: [run], gates: [gate], gatesPage: { total: 1 } } };
}

function bootstrapSession(value?: BootstrapState) {
  return reactive({
    authenticatedBootstrap: value,
    takeAuthenticatedBootstrap(): BootstrapState | undefined {
      const snapshot = this.authenticatedBootstrap;
      this.authenticatedBootstrap = undefined;
      return snapshot;
    },
  });
}

beforeEach(() => setActivePinia(createPinia()));

describe("проверенный session bootstrap до realtime", () => {
  it("начальный RUN SYSTEM и ORG gate принимаются раньше SYSTEM_ASSISTANT snapshot", () => {
    const platform = usePlatformStore();
    expect(() =>
      platform.applyPlatformSnapshot("RUN", undefined, snapshot()),
    ).toThrow("anchor");
    const session = bootstrapSession();
    const stop = synchronizeSessionBootstrap(session, platform);
    session.authenticatedBootstrap = authenticatedBootstrap();
    expect(session.authenticatedBootstrap).toBeUndefined();
    expect(platform.bootstrap?.organizationRef).toBe("org_fixture");
    expect(() =>
      platform.applyPlatformSnapshot("RUN", undefined, snapshot()),
    ).not.toThrow();
    expect(platform.runs.run_fixture?.assistantPin?.organizationRef).toBe(
      "org_fixture",
    );
    expect(platform.gates.gate_fixture?.scopeKind).toBe("ORGANIZATION");
    stop();
  });

  it("foreign organization остаётся закрытой после bootstrap", () => {
    const platform = usePlatformStore();
    const stop = synchronizeSessionBootstrap(
      bootstrapSession(authenticatedBootstrap()),
      platform,
    );
    expect(() =>
      platform.applyPlatformSnapshot(
        "RUN",
        undefined,
        snapshot(systemRun("org_foreign")),
      ),
    ).toThrow("pin");
    expect(platform.runs).toEqual({});
    expect(() =>
      platform.applyPlatformSnapshot(
        "RUN",
        undefined,
        snapshot(systemRun(), organizationGate("org_foreign")),
      ),
    ).toThrow("scope");
    expect(platform.gates).toEqual({});
    stop();
  });

  it("возврат из public route сохраняет последнюю realtime revision, а не старый session bootstrap", () => {
    const platform = usePlatformStore();
    const session = bootstrapSession(authenticatedBootstrap());
    const stopFirst = synchronizeSessionBootstrap(session, platform);
    const latest = {
      ...authenticatedBootstrap(),
      assistant: { ...authenticatedBootstrap().assistant, version: 9 },
    };
    platform.applyPlatformSnapshot("SYSTEM_ASSISTANT", undefined, {
      assistant: { assistant: latest.assistant },
      conversations: { conversations: [], page: {} },
      bootstrap: { state: latest },
    });
    stopFirst();
    expect(session.authenticatedBootstrap).toBeUndefined();
    const stopNext = synchronizeSessionBootstrap(session, platform);
    expect(() =>
      platform.applyPlatformSnapshot("RUN", undefined, snapshot()),
    ).not.toThrow();
    expect(platform.bootstrap?.organizationRef).toBe("org_fixture");
    expect(platform.bootstrap?.assistant.version).toBe(9);
    expect(platform.assistant?.version).toBe(9);
    stopNext();
  });
});
