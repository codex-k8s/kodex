import { expect, type Page, type WebSocketRoute } from "@playwright/test";
import type {
  AssistantConversation,
  AssistantTurn,
  SystemAssistant,
} from "../../src/shared/api/generated/openapi/types.gen";

export const projectA = "prj_concurrency_a";
export const projectB = "prj_concurrency_b";
export const now = "2026-10-04T00:00:00Z";
export const assistant: SystemAssistant = {
  ref: "ast_concurrency",
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
export function conversationRef(project: string, index: number): string {
  return `cnv_${project === projectA ? "a" : "b"}_${String(index).padStart(2, "0")}`;
}
function makeConversation(
  project: string,
  index: number,
): AssistantConversation {
  const ref = conversationRef(project, index);
  return {
    ref,
    version: 1,
    title: `Диалог ${project === projectA ? "A" : "B"}-${String(index)}`,
    assistantScope: "SYSTEM",
    assistantRef: assistant.ref,
    state: "ACTIVE",
    titleSource: "USER_EDITED",
    titleRevision: 1,
    projectRef: project,
    context: {
      route: `/projects/${project}`,
      entityKind: "PROJECT",
      entityRef: project,
      entityName: project,
      entityVersion: 1,
      allowedOperations: [],
    },
    turns: [
      {
        source: { origin: "ORDINARY" as const },
        ref: `turn_${ref}_1`,
        sequence: 1,
        role: "USER",
        content: `История ${ref}`,
        state: index <= 2 ? "RUNNING" : "QUEUED",
        runRef: `run_${ref}_1`,
        runVersion: 1,
        createdAt: now,
      },
    ],
    updatedAt: now,
  };
}
interface Resume {
  type: "SESSION_RESUME";
  requestRef: string;
  projectRef: string;
  platformAfterSequence: number;
  platformSnapshotRequired: boolean;
  runs: Array<{ runRef: string; afterSequence: number }>;
}
interface Connection {
  socket: WebSocketRoute;
  resume?: Resume;
  closed: boolean;
}
export interface Mutation {
  path: string;
  conversationRef: string;
  mode?: string;
  content?: string;
}

// Изолированный типизированный владелец оснастки: только HTTP/WS, без inference.
// Его переходы не являются доказательством backend admission/authority.
export class AssistantConcurrencyNetwork {
  readonly values = new Map<string, AssistantConversation>();
  readonly mutations: Mutation[] = [];
  readonly resumes: Resume[] = [];
  readonly unexpected: string[] = [];
  readonly connections: Connection[] = [];
  reads = 0;
  sequence = 1;
  epoch = 1;
  constructor() {
    for (const project of [projectA, projectB])
      for (let index = 1; index <= (project === projectA ? 11 : 2); index++) {
        const value = makeConversation(project, index);
        this.values.set(value.ref, value);
      }
  }
  conversations(project: string): AssistantConversation[] {
    return structuredClone(
      [...this.values.values()].filter(
        (value) => value.projectRef === project && value.state === "ACTIVE",
      ),
    );
  }
  private envelope(
    connection: Connection,
    type: string,
  ): Record<string, unknown> {
    if (!connection.resume)
      throw new Error("Synthetic connection is not resumed");
    return {
      type,
      requestRef: connection.resume.requestRef,
      streamKind: "PLATFORM",
      streamRef: "PLATFORM",
      cursor: this.sequence,
    };
  }
  private snapshot(
    connection: Connection,
    mode: "BOOTSTRAP" | "DELTA",
    project = connection.resume?.projectRef,
  ): void {
    if (!project) throw new Error("Synthetic snapshot project is missing");
    connection.socket.send(
      JSON.stringify({
        ...this.envelope(connection, "PLATFORM_SNAPSHOT"),
        mode,
        kind: "SYSTEM_ASSISTANT",
        projectRef: project,
        ...(mode === "DELTA"
          ? { eventName: "ASSISTANT_CONVERSATION_CHANGED" }
          : {}),
        snapshot: {
          assistant: { assistant },
          conversations: {
            conversations: this.conversations(project),
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
  admitTen(): void {
    for (let index = 3; index <= 10; index++) {
      const value = this.values.get(conversationRef(projectA, index));
      if (!value) throw new Error("Synthetic conversation is missing");
      const first = value.turns[0];
      if (!first) throw new Error("Synthetic first turn is missing");
      first.state = "RUNNING";
      value.version++;
    }
    this.publish();
  }
  retryReadback(ref: string): void {
    const value = this.values.get(ref);
    if (!value) throw new Error("Synthetic retry conversation is missing");
    value.turns.push({
      source: { origin: "ORDINARY" as const },
      ref: `turn_${ref}_retry_${String(this.epoch)}`,
      sequence: value.turns.length + 1,
      role: "USER",
      content: `Повторный ход ${ref}`,
      state: "RUNNING",
      runRef: `run_${ref}_retry_${String(this.epoch)}`,
      runVersion: 1,
      createdAt: now,
    });
    value.version++;
    this.publish();
  }
  async restart(): Promise<void> {
    this.epoch++;
    for (const connection of this.connections.filter(
      (value) => !value.closed,
    )) {
      connection.closed = true;
      await connection.socket.close({
        code: 1012,
        reason: "SYNTHETIC_SERVER_RESTART",
      });
    }
  }
  sendForeignProjectSnapshot(): void {
    for (const connection of this.connections)
      if (!connection.closed && connection.resume)
        this.snapshot(
          connection,
          "DELTA",
          connection.resume.projectRef === projectA ? projectB : projectA,
        );
  }
  sendStaleRequestSnapshot(requestRef: string): void {
    for (const connection of this.connections) {
      if (connection.closed || !connection.resume) continue;
      const conversations = this.conversations(connection.resume.projectRef);
      const first = conversations[0];
      if (!first) throw new Error("Synthetic stale conversation is missing");
      first.version += 100;
      first.title = "Недопустимый устаревший snapshot";
      connection.socket.send(
        JSON.stringify({
          ...this.envelope(connection, "PLATFORM_SNAPSHOT"),
          requestRef,
          mode: "DELTA",
          kind: "SYSTEM_ASSISTANT",
          eventName: "ASSISTANT_CONVERSATION_CHANGED",
          projectRef: connection.resume.projectRef,
          snapshot: {
            assistant: { assistant },
            conversations: { conversations, page: {} },
          },
        }),
      );
    }
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
        // onClose заменяет автоматическую пересылку close: оснастка обязана
        // завершить handshake, иначе браузер остаётся в CLOSING до timeout.
        void socket.close({ code: code ?? 1000, reason: reason ?? "" });
      });
      socket.onMessage((message) => {
        const resume = JSON.parse(String(message)) as Resume;
        expect(resume.type).toBe("SESSION_RESUME");
        expect(resume.requestRef).toMatch(/^[a-f\d]{32}$/);
        expect([projectA, projectB]).toContain(resume.projectRef);
        expect(resume.runs).toEqual([]);
        connection.resume = resume;
        this.resumes.push(structuredClone(resume));
        this.snapshot(connection, "BOOTSTRAP");
        socket.send(
          JSON.stringify({
            ...this.envelope(connection, "PLATFORM_READY"),
            availableKinds: ["SYSTEM_ASSISTANT"],
          }),
        );
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
            ],
          }),
        );
      });
    });
    await page.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
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
              redirectUri: "https://kodex.test/auth/callback",
              postLogoutRedirectUri: "https://kodex.test/",
              scope: "openid profile email",
            },
          },
        });
      if (url.pathname === "/api/v1/session/ticket") {
        expect(request.method()).toBe("POST");
        expect(request.headers()["x-csrf-token"]).toBe("c".repeat(43));
        return route.fulfill({
          json: {
            ticket: "t".repeat(43),
            expiresAt: new Date(Date.now() + 60000).toISOString(),
          },
        });
      }
      if (url.pathname === "/api/v1/system-assistant")
        return route.fulfill({ json: assistant });
      if (
        url.pathname === "/api/v1/assistant-conversations" &&
        request.method() === "GET"
      ) {
        this.reads++;
        const project = url.searchParams.get("projectRef");
        expect([projectA, projectB]).toContain(project);
        if (!project)
          throw new Error("Synthetic conversation project is missing");
        expect(url.searchParams.get("assistantScope")).toBe("SYSTEM");
        // При первой загрузке SYSTEM ref ещё загружается параллельно: его
        // назначает владелец. Следующие чтения обязаны сохранять точный pin.
        expect([null, assistant.ref]).toContain(
          url.searchParams.get("assistantRef"),
        );
        return route.fulfill({ json: { items: this.conversations(project) } });
      }
      const match =
        /^\/api\/v1\/assistant-conversations\/([^/]+)\/(turns|active-turn\/cancellation|archive)$/.exec(
          url.pathname,
        );
      if (match) {
        expect(request.method()).toBe("POST");
        expect(request.headers()["x-csrf-token"]).toBe("c".repeat(43));
        expect(request.headers()["idempotency-key"]).toBeTruthy();
        const ref = match[1];
        if (!ref) throw new Error("Synthetic conversation ref is missing");
        const value = this.values.get(ref);
        if (!value) throw new Error("Unknown synthetic conversation");
        if (match[2] === "turns") {
          const body = request.postDataJSON() as {
            content: string;
            deliveryMode: "QUEUE" | "INTERRUPT_ACTIVE";
            context: AssistantConversation["context"];
          };
          expect(body.context.entityRef).toBe(value.projectRef);
          expect(["QUEUE", "INTERRUPT_ACTIVE"]).toContain(body.deliveryMode);
          this.mutations.push({
            path: url.pathname,
            conversationRef: ref,
            mode: body.deliveryMode,
            content: body.content,
          });
          if (body.deliveryMode === "INTERRUPT_ACTIVE")
            for (const turn of value.turns)
              if (turn.state === "RUNNING") turn.state = "CANCELLED";
          const turn: AssistantTurn = {
            source: { origin: "ORDINARY" as const },
            ref: `turn_${ref}_${String(value.turns.length + 1)}`,
            sequence: value.turns.length + 1,
            role: "USER",
            content: body.content,
            state: body.deliveryMode === "QUEUE" ? "QUEUED" : "RUNNING",
            runRef: `run_${ref}_${String(value.turns.length + 1)}`,
            runVersion: 1,
            createdAt: now,
          };
          value.turns.push(turn);
          value.version++;
          const response = structuredClone(value);
          await route.fulfill({ json: response });
          this.publish();
          return;
        }
        expect(request.headers()["if-match"]).toBe(
          `"${String(value.version)}"`,
        );
        this.mutations.push({ path: url.pathname, conversationRef: ref });
        const active = value.turns.find((turn) => turn.state === "RUNNING");
        if (match[2] === "archive") {
          value.state = "ARCHIVED";
          for (const turn of value.turns)
            if (["RUNNING", "QUEUED"].includes(turn.state))
              turn.state = "CANCELLED";
          value.version++;
          await route.fulfill({ json: structuredClone(value) });
        } else {
          if (active) active.state = "CANCELLED";
          value.version++;
          await route.fulfill({
            json: {
              conversationRef: ref,
              runRef: active?.runRef ?? "",
              cancelled: Boolean(active),
            },
          });
        }
        this.publish();
        return;
      }
      if (url.pathname.startsWith("/api/")) {
        this.unexpected.push(`${request.method()} ${url.pathname}`);
        return route.fulfill({
          status: 501,
          json: { title: "Unexpected synthetic request", status: 501 },
        });
      }
      if (url.origin !== "https://kodex.test")
        throw new Error("External synthetic request is forbidden");
      await route.fulfill({
        response: await route.fetch({
          url: `http://127.0.0.1:43122${url.pathname}${url.search}`,
        }),
      });
    });
  }
}
