import { expect, type Page, type WebSocketRoute } from "@playwright/test";
import type {
  Agent,
  Artifact,
  AssistantConversation,
  AssistantScope,
  BootstrapState,
  ProjectAssistantProfile,
  Run,
  RunGraph,
  RunWorkspace,
  SystemAssistant,
} from "../../src/shared/api/generated/openapi/types.gen";

const now = "2026-10-04T00:00:00Z";
export const failedRunRef = "run_retry_original",
  retriedRunRef = "run_retry_returned",
  firstConversationRef = "cnv_retry_01";
const organizationRef = "org_retry_fixture",
  projectRef = "prj_retry_fixture",
  sessionRef = "ses_retry_fixture";
const systemAssistant: SystemAssistant = {
  ref: "ast_retry_fixture",
  version: 1,
  name: "Kodex",
  system: true,
  removable: false,
  corePromptRevision: "synthetic-v1",
  ownerInstructions: "",
  runtimeState: "READY",
  readinessSummary: "Ready",
  nextActions: ["CREATE_CONVERSATION", "ADD_TURN"],
};
const profile: ProjectAssistantProfile = {
  ref: "asstp_retry_fixture",
  projectRef,
  agentRef: "agt_retry_fixture",
  name: "Помощник проекта",
  state: "ACTIVE",
  version: 1,
  createdAt: now,
  updatedAt: now,
};
const agent: Agent = {
  ref: profile.agentRef,
  projectRef,
  name: profile.name,
  version: 1,
  purpose: "Проверка",
  roleDescription: "Помощник",
  state: "READY",
  enabled: true,
  system: false,
  runtimeRef: "env_retry_fixture",
  runtimeName: "Проверочный",
  runtimeReady: true,
  capabilities: [],
  integrations: [],
  knowledgeArtifactRefs: [],
  nextActions: ["LAUNCH"],
  updatedAt: now,
};
interface Resume {
  type: string;
  requestRef: string;
  projectRef?: string;
  runs?: Array<{ runRef: string; afterSequence: number }>;
  runRef?: string;
}
interface Connection {
  socket: WebSocketRoute;
  resume?: Resume;
  closed: boolean;
}
export interface RetryCommand {
  runRef: string;
  version: string | undefined;
  idempotencyKey: string | undefined;
  status: number;
}

// Проверяет реальный FE transport/cache. Переходы этой оснастки не доказывают backend lifecycle.
export class AssistantRetryNetwork {
  readonly conversations = new Map<string, AssistantConversation>();
  readonly runs = new Map<string, RunWorkspace>();
  readonly commands: RetryCommand[] = [];
  readonly unexpected: string[] = [];
  readonly connections: Connection[] = [];
  readonly resumes: Resume[] = [];
  readonly reads: string[] = [];
  readonly receipts = new Map<string, RunWorkspace>();
  sequence = 1;
  loseAcknowledgement = false;
  constructor(readonly scope: AssistantScope) {
    for (let index = 1; index <= 10; index++) {
      const ref = `cnv_retry_${String(index).padStart(2, "0")}`;
      const runRef =
        index === 1 ? failedRunRef : `run_retry_neighbor_${String(index)}`;
      this.conversations.set(ref, {
        ref,
        version: 1,
        title: `Диалог ${String(index)}`,
        state: "ACTIVE",
        assistantScope: scope,
        assistantRef:
          scope === "PROJECT" ? profile.agentRef : systemAssistant.ref,
        ...(scope === "PROJECT"
          ? { projectRef, assistantProfileRef: profile.ref }
          : {}),
        titleSource: "USER_EDITED",
        titleRevision: 1,
        context: {
          route:
            scope === "PROJECT" ? `/projects/${projectRef}` : "/onboarding",
          entityKind: scope === "PROJECT" ? "PROJECT" : "PLATFORM",
          entityRef: scope === "PROJECT" ? projectRef : "",
          entityName: "Проверка retry",
          entityVersion: 1,
          allowedOperations: [],
        },
        turns: [
          {
            source: { origin: "ORDINARY" as const },
            ref: `turn_retry_${String(index)}`,
            sequence: 1,
            role: "USER",
            content: `История ${ref}`,
            state: index === 1 ? "FAILED" : "RUNNING",
            runRef,
            runVersion: 1,
            createdAt: now,
          },
        ],
        updatedAt: now,
      });
    }
    this.runs.set(failedRunRef, this.workspace(failedRunRef, "FAILED", 1));
  }
  workspace(ref: string, state: Run["state"], attempt: number): RunWorkspace {
    const artifactRef = "art_retry_child";
    const run: Run = {
      ref,
      version: 1,
      ...(this.scope === "PROJECT" ? { projectRef } : {}),
      assistantPin: {
        scope: this.scope,
        organizationRef,
        conversationRef: firstConversationRef,
        assistantRef:
          this.scope === "PROJECT" ? profile.agentRef : systemAssistant.ref,
        ...(this.scope === "PROJECT"
          ? { projectRef, profileRef: profile.ref }
          : {}),
      },
      sessionRef,
      rootRunRef: ref,
      ...(attempt > 1 ? { retryOfRunRef: failedRunRef } : {}),
      target: {
        type: "SYSTEM_ASSISTANT",
        ref: this.scope === "PROJECT" ? profile.agentRef : systemAssistant.ref,
        displayName: "Kodex",
        version: 1,
      },
      title: "Проверка повторного хода",
      titleSource: "USER_EDITED",
      activitySummary: "",
      inputSummary: "Проверочный ход",
      state,
      source: "SYSTEM_ASSISTANT",
      initiator: { ref: "usr_retry_fixture", displayName: "Тестовый владелец" },
      attempt,
      graphRevision: 1,
      lastEventSequence: 0,
      usage: {
        inputTokens: 0,
        cachedInputTokens: 0,
        cacheWriteInputTokens: 0,
        modelContextWindow: 0,
        outputTokens: 0,
        reasoningOutputTokens: 0,
        totalTokens: 0,
      },
      artifactRefs: [],
      gateRefs: [],
      createdAt: now,
      nextActions: state === "FAILED" ? ["RETRY"] : ["CANCEL"],
    };
    const currentNode = {
      ref: `node_${ref}`,
      runRef: ref,
      type: "AGENT_EXECUTION" as const,
      state: state === "FAILED" ? ("FAILED" as const) : ("QUEUED" as const),
      displayName: "Kodex",
      agentRef: run.target.ref,
      attempt,
      artifactRefs: [artifactRef],
      childRunRefs: [],
      createdAt: now,
      nextActions: [],
    };
    const graph: RunGraph = {
      runRef: ref,
      revision: 1,
      sequence: 0,
      nodes: [currentNode],
      edges: [],
    };
    if (attempt > 1) {
      graph.nodes.unshift({
        ...currentNode,
        ref: `node_${failedRunRef}`,
        runRef: failedRunRef,
        state: "FAILED",
        attempt: 1,
        artifactRefs: [],
      });
      graph.edges.push({
        ref: "edge_retry_lineage",
        runRef: ref,
        sourceNodeRef: `node_${failedRunRef}`,
        targetNodeRef: currentNode.ref,
        type: "RETRY_OF",
        label: "Повтор",
      });
    }
    return { run, graph };
  }
  staleVersion(): void {
    const run = this.runs.get(failedRunRef)?.run;
    if (!run) throw new Error("Synthetic original run is missing");
    run.version++;
  }
  private snapshot(connection: Connection, mode: "BOOTSTRAP" | "DELTA"): void {
    const resume = connection.resume;
    if (!resume) throw new Error("Synthetic session is not resumed");
    connection.socket.send(
      JSON.stringify({
        type: "PLATFORM_SNAPSHOT",
        requestRef: resume.requestRef,
        streamKind: "PLATFORM",
        streamRef: "PLATFORM",
        cursor: this.sequence,
        mode,
        kind: "SYSTEM_ASSISTANT",
        ...(resume.projectRef ? { projectRef: resume.projectRef } : {}),
        ...(mode === "DELTA"
          ? { eventName: "ASSISTANT_CONVERSATION_CHANGED" }
          : {}),
        snapshot: {
          assistant: { assistant: systemAssistant },
          conversations: {
            conversations: [...this.conversations.values()],
            page: {},
          },
        },
      }),
    );
  }
  publish(): void {
    this.sequence++;
    for (const connection of this.connections)
      if (!connection.closed && connection.resume)
        this.snapshot(connection, "DELTA");
  }
  async restart(): Promise<void> {
    for (const connection of this.connections)
      if (!connection.closed) {
        connection.closed = true;
        await connection.socket.close({
          code: 1012,
          reason: "SYNTHETIC_SERVER_RESTART",
        });
      }
  }
  private runReady(
    socket: WebSocketRoute,
    runRef: string,
    requestRef: string,
  ): void {
    const workspace = this.runs.get(runRef);
    if (!workspace) throw new Error("Unknown synthetic run subscription");
    socket.send(
      JSON.stringify({
        type: "RUN_GRAPH_SNAPSHOT",
        requestRef,
        streamKind: "RUN",
        streamRef: runRef,
        cursor: workspace.graph.sequence,
        snapshot: workspace.graph,
      }),
    );
    socket.send(
      JSON.stringify({
        type: "RUN_READY",
        requestRef,
        streamKind: "RUN",
        streamRef: runRef,
        cursor: 0,
      }),
    );
  }
  async install(page: Page): Promise<void> {
    await page.context().addCookies([
      {
        name: "__Host-kodex-csrf",
        value: "c".repeat(43),
        domain: "kodex.test",
        path: "/",
        secure: true,
        sameSite: "Strict",
      },
    ]);
    await page.routeWebSocket("**/api/v1/session/stream*", (socket) => {
      const connection: Connection = { socket, closed: false };
      this.connections.push(connection);
      socket.onClose((code, reason) => {
        connection.closed = true;
        void socket.close({ code: code ?? 1000, reason: reason ?? "" });
      });
      socket.onMessage((message) => {
        const resume = JSON.parse(String(message)) as Resume;
        if (resume.type === "UNSUBSCRIBE_RUN") return;
        if (resume.type === "SUBSCRIBE_RUN") {
          if (!resume.runRef)
            throw new Error("Missing synthetic subscription run");
          this.runReady(socket, resume.runRef, resume.requestRef);
          return;
        }
        expect(resume.type).toBe("SESSION_RESUME");
        expect(resume.projectRef).toBe(
          this.scope === "PROJECT" ? projectRef : undefined,
        );
        connection.resume = resume;
        this.resumes.push(structuredClone(resume));
        this.snapshot(connection, "BOOTSTRAP");
        socket.send(
          JSON.stringify({
            type: "PLATFORM_SNAPSHOT",
            requestRef: resume.requestRef,
            streamKind: "PLATFORM",
            streamRef: "PLATFORM",
            cursor: this.sequence,
            mode: "BOOTSTRAP",
            kind: "RUN",
            ...(resume.projectRef ? { projectRef: resume.projectRef } : {}),
            snapshot: {
              catalog: {
                runs: [...this.runs.values()].map((workspace) => workspace.run),
                gates: [],
                runsPage: {},
                gatesPage: {},
              },
            },
          }),
        );
        socket.send(
          JSON.stringify({
            type: "PLATFORM_READY",
            requestRef: resume.requestRef,
            streamKind: "PLATFORM",
            streamRef: "PLATFORM",
            cursor: this.sequence,
            availableKinds: ["SYSTEM_ASSISTANT", "RUN"],
          }),
        );
        for (const run of resume.runs ?? [])
          this.runReady(socket, run.runRef, resume.requestRef);
        socket.send(
          JSON.stringify({
            type: "SESSION_READY",
            requestRef: resume.requestRef,
            streams: [
              {
                streamKind: "PLATFORM",
                streamRef: "PLATFORM",
                cursor: this.sequence,
              },
              ...(resume.runs ?? []).map((run) => ({
                streamKind: "RUN",
                streamRef: run.runRef,
                cursor: 0,
              })),
            ],
          }),
        );
      });
    });
    await page.route("**/*", async (route) => {
      const request = route.request(),
        url = new URL(request.url());
      if (url.origin !== "https://kodex.test")
        throw new Error("External synthetic origin is forbidden");
      if (url.pathname === "/config/runtime-config.json")
        return route.fulfill({
          json: {
            revision: "0".repeat(64),
            environment: "synthetic",
            apiBaseUrl: "/",
            realtimeUrl: "/api/v1",
            requestTimeoutMs: 10000,
            oidc: {
              authority: "https://identity.kodex.test/realms/kodex",
              clientId: "synthetic",
              redirectUri: "/auth/callback",
              postLogoutRedirectUri: "/",
              scope: "openid",
            },
          },
        });
      if (url.pathname === "/api/v1/bootstrap") {
        const bootstrap: BootstrapState = {
          organizationRef,
          initialized: true,
          onboardingComplete: true,
          webOnlyReady: true,
          assistant: systemAssistant,
          currentUser: {
            ref: "usr_retry_fixture",
            displayName: "Тестовый владелец",
          },
          platformRole: "OWNER",
          nextActions: [],
          speechTranscription: { available: false, reason: "STT_DISABLED" },
        };
        return route.fulfill({ json: bootstrap });
      }
      if (url.pathname === "/api/v1/session/ticket")
        return route.fulfill({
          json: {
            ticket: "t".repeat(43),
            expiresAt: new Date(Date.now() + 60000).toISOString(),
          },
        });
      if (url.pathname === "/api/v1/system-assistant")
        return route.fulfill({ json: systemAssistant });
      if (url.pathname === `/api/v1/projects/${projectRef}/assistant`)
        return route.fulfill({ json: profile });
      if (url.pathname === `/api/v1/agents/${profile.agentRef}`)
        return route.fulfill({ json: agent });
      if (url.pathname === "/api/v1/assistant-conversations") {
        expect(url.searchParams.get("assistantScope")).toBe(this.scope);
        expect(url.searchParams.get("projectRef")).toBe(
          this.scope === "PROJECT" ? projectRef : null,
        );
        return route.fulfill({
          json: { items: [...this.conversations.values()] },
        });
      }
      const conversationMatch =
        /^\/api\/v1\/assistant-conversations\/([^/]+)$/.exec(url.pathname);
      if (conversationMatch?.[1]) {
        const value = this.conversations.get(conversationMatch[1]);
        if (!value) throw new Error("Unknown synthetic conversation read");
        return route.fulfill({ json: value });
      }
      const runMatch =
        /^\/api\/v1\/runs\/([^/]+)\/(graph|events|commands)$/.exec(
          url.pathname,
        );
      if (runMatch?.[1]) {
        const ref = runMatch[1],
          workspace = this.runs.get(ref);
        if (!workspace) throw new Error("Unknown synthetic run read");
        if (runMatch[2] === "graph") {
          this.reads.push(ref);
          return route.fulfill({ json: workspace });
        }
        if (runMatch[2] === "events")
          return route.fulfill({ json: { items: [], complete: true } });
        expect(request.method()).toBe("POST");
        expect(request.postDataJSON()).toEqual({ action: "RETRY" });
        expect(request.headers()["x-csrf-token"]).toBe("c".repeat(43));
        const idempotencyKey = request.headers()["idempotency-key"],
          version = request.headers()["if-match"];
        expect(idempotencyKey).toBeTruthy();
        const receipt = idempotencyKey
          ? this.receipts.get(idempotencyKey)
          : undefined;
        if (receipt) {
          this.commands.push({
            runRef: ref,
            version,
            idempotencyKey,
            status: 200,
          });
          return route.fulfill({ json: receipt });
        }
        if (version !== `"${String(workspace.run.version)}"`) {
          this.commands.push({
            runRef: ref,
            version,
            idempotencyKey,
            status: 412,
          });
          return route.fulfill({
            status: 412,
            json: {
              title: "Run version changed",
              status: 412,
              code: "VERSION_OR_STATE_CONFLICT",
              retryable: true,
            },
          });
        }
        expect(ref).toBe(failedRunRef);
        expect(workspace.run.state).toBe("FAILED");
        const value = this.conversations.get(firstConversationRef);
        if (!value) throw new Error("Synthetic retry conversation is missing");
        const retried = this.workspace(retriedRunRef, "QUEUED", 2);
        this.runs.set(retriedRunRef, retried);
        value.turns.push({
          source: { origin: "ORDINARY" as const },
          ref: "turn_retry_attempt_2",
          sequence: value.turns.length + 1,
          role: "USER",
          content: value.turns[0]?.content ?? "",
          state: "QUEUED",
          runRef: retriedRunRef,
          runVersion: retried.run.version,
          createdAt: now,
        });
        value.version++;
        if (idempotencyKey)
          this.receipts.set(idempotencyKey, structuredClone(retried));
        const lose = this.loseAcknowledgement;
        this.loseAcknowledgement = false;
        this.commands.push({
          runRef: ref,
          version,
          idempotencyKey,
          status: lose ? 503 : 200,
        });
        await route.fulfill(
          lose
            ? {
                status: 503,
                json: {
                  title: "Acknowledgement unavailable",
                  status: 503,
                  code: "RUN_UNAVAILABLE",
                  retryable: true,
                },
              }
            : { json: retried },
        );
        this.publish();
        return;
      }
      if (url.pathname === "/api/v1/owner-gates")
        return route.fulfill({ json: { items: [], total: 0 } });
      if (url.pathname === "/api/v1/artifacts/art_retry_child") {
        const artifact: Artifact = {
          ref: "art_retry_child",
          currentRevisionRef: "arv_fixture_retry_child",
          version: 1,
          ...(this.scope === "PROJECT" ? { projectRef } : {}),
          runRef: "run_retry_child",
          sessionRef,
          fileName: "child-result.txt",
          mediaType: "text/plain",
          sizeBytes: 24,
          digest: "a".repeat(64),
          scanState: "CLEAN",
          source: "AGENT_RESULT",
          revision: 1,
          lifecycleState: "ACTIVE",
          agentBindings: [],
          previewAvailable: false,
          createdAt: now,
          nextActions: [],
        };
        return route.fulfill({ json: artifact });
      }
      if (url.pathname.startsWith("/api/")) {
        this.unexpected.push(`${request.method()} ${url.pathname}`);
        return route.fulfill({
          status: 501,
          json: { title: "Unexpected synthetic request", status: 501 },
        });
      }
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
  }
}
