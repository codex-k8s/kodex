import { describe, expect, it } from "vitest";
import {
  buildAssistantChatTimeline,
  activeAssistantChatItemId,
} from "./chat-timeline";
import {
  buildRunTranscriptItems,
  executionKey,
} from "@/features/runs/run-activity";
import type {
  AssistantConversation,
  AssistantTurn,
  Run,
  RunEvent,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

const turn = (
  ref: string,
  sequence: number,
  runRef: string,
  role: AssistantTurn["role"] = "USER",
): AssistantTurn => ({
  source: { origin: "ORDINARY" as const },
  ref,
  sequence,
  runRef,
  role,
  runVersion: 2,
  content: ref,
  state: "COMPLETED",
  createdAt: "2026-10-04T12:00:00Z",
});
const first = turn("trn_first_fixture", 1, "run_first_fixture");
const final = turn("trn_final_fixture", 2, first.runRef ?? "", "ASSISTANT");
const second = turn("trn_second_fixture", 3, "run_second_fixture");
const conversation: AssistantConversation = {
  ref: "cnv_owned_fixture",
  version: 2,
  title: "Диалог",
  state: "ACTIVE",
  assistantScope: "SYSTEM",
  assistantRef: "agt_owned_fixture",
  titleSource: "SERVER_DEFAULT",
  titleRevision: 1,
  context: {
    route: "/",
    entityKind: "",
    entityRef: "",
    entityName: "",
    allowedOperations: [],
  },
  turns: [first, final, second],
  updatedAt: first.createdAt,
};
function run(anchor: AssistantTurn): Run {
  return {
    ref: anchor.runRef ?? "",
    rootRunRef: anchor.runRef ?? "",
    sessionRef: "ses_owned_fixture",
    version: 2,
    target: {
      type: "SYSTEM_ASSISTANT",
      ref: conversation.assistantRef,
      displayName: "Помощник",
      version: 1,
    },
    assistantPin: {
      scope: "SYSTEM",
      organizationRef: "org_owned_fixture",
      conversationRef: conversation.ref,
      assistantRef: conversation.assistantRef,
    },
    title: "Ход",
    activitySummary: "Проверка",
    titleSource: "SERVER_DEFAULT",
    state: "RUNNING",
    source: "SYSTEM_ASSISTANT",
    initiator: { ref: "usr_owned_fixture", displayName: "Владелец" },
    attempt: 1,
    graphRevision: 1,
    lastEventSequence: 3,
    usage: {
      totalTokens: 0,
      inputTokens: 0,
      cachedInputTokens: 0,
      cacheWriteInputTokens: 0,
      outputTokens: 0,
      reasoningOutputTokens: 0,
      modelContextWindow: 0,
    },
    artifactRefs: [],
    gateRefs: [],
    nextActions: [],
    createdAt: anchor.createdAt,
  };
}
function event(anchor: AssistantTurn, sequence: number): RunEvent {
  return {
    ref: `evt_${anchor.ref}_${String(sequence)}`,
    runRef: anchor.runRef ?? "",
    sequence,
    type: "TURN_PROGRESS",
    graphRevision: 1,
    occurredAt: anchor.createdAt,
    summary: "Комментарий",
    messageKind: "INTERMEDIATE_MESSAGE",
    nodeState: "RUNNING",
    run: run(anchor),
    execution: {
      runRef: anchor.runRef ?? "",
      nodeRef: `nod_${anchor.ref}`,
      sessionRef: "ses_owned_fixture",
      turnRef: anchor.ref,
      turnNumber: anchor.sequence,
      attempt: 1,
    },
    message: {
      source: { origin: "ORDINARY" as const },
      ref: `msg_${anchor.ref}_${String(sequence)}`,
      phase: "COMMENTARY",
      revision: 1,
      text: "Комментарий",
    },
  };
}
function graph(anchor: AssistantTurn): RunGraph {
  return {
    runRef: anchor.runRef ?? "",
    revision: 1,
    sequence: 3,
    edges: [],
    nodes: [
      {
        ref: `nod_${anchor.ref}`,
        runRef: anchor.runRef ?? "",
        turnRef: anchor.ref,
        agentRef: conversation.assistantRef,
        attempt: 1,
        type: "AGENT_EXECUTION",
        state: "RUNNING",
        displayName: "Помощник",
        artifactRefs: [],
        childRunRefs: [],
        nextActions: [],
        createdAt: anchor.createdAt,
      },
    ],
  };
}

describe("server-owned callback continuation", () => {
  it("сворачивает один exact callback receipt и повторы, сохраняя USER/final", () => {
    const callback = turn(
      "trn_callback_fixture",
      3,
      first.runRef ?? "",
      "SYSTEM_RECEIPT",
    );
    callback.source = { origin: "CALLBACK_CONTINUATION" };
    callback.content = "PRIVATE_CALLBACK_PROMPT";
    const execution = event(callback, 20);
    execution.type = "TURN_QUEUED";
    execution.actor = {
      kind: "AGENT",
      ref: conversation.assistantRef,
      name: "Помощник",
    };
    if (!execution.execution)
      throw new Error("Missing callback execution fixture");
    execution.execution.attempt = 2;
    execution.message = {
      ref: callback.ref,
      phase: "USER",
      revision: 1,
      text: "PRIVATE_CALLBACK_PROMPT",
      source: callback.source,
    };
    const callbackGraph = graph(callback);
    const callbackNode = callbackGraph.nodes[0];
    if (!callbackNode) throw new Error("Missing callback node fixture");
    callbackNode.attempt = 2;
    const current = { ...conversation, turns: [first, final, callback] };
    const ownerRun = run(first);
    const timeline = buildAssistantChatTimeline(
      current,
      current.turns,
      "org_owned_fixture",
      { [ownerRun.ref]: ownerRun },
      { [ownerRun.ref]: callbackGraph },
      [execution, { ...execution, ref: "evt_repeat_fixture", sequence: 21 }],
    );
    expect(
      timeline
        .filter((entry) => entry.kind === "TURN")
        .map((entry) => entry.turn.ref),
    ).toEqual([first.ref, final.ref]);
    const items = buildRunTranscriptItems(
      timeline.flatMap((entry) =>
        entry.kind === "ACTIVITY" ? entry.events : [],
      ),
    );
    expect(items).toHaveLength(1);
    expect(items[0]?.kind).toBe("agent");
    expect(items[0]?.summary).toBe("i18n:CALLBACK_CONTINUATION_PUBLIC");
    expect(JSON.stringify(items)).not.toContain("PRIVATE_CALLBACK_PROMPT");
  });

  it("не сворачивает чужой run/pin и обычную реплику по её тексту", () => {
    const callback = turn(
      "trn_callback_fixture",
      3,
      first.runRef ?? "",
      "SYSTEM_RECEIPT",
    );
    callback.source = { origin: "CALLBACK_CONTINUATION" };
    const execution = event(callback, 20);
    execution.actor = {
      kind: "AGENT",
      ref: conversation.assistantRef,
      name: "Помощник",
    };
    execution.message = {
      ref: callback.ref,
      phase: "USER",
      revision: 1,
      text: "Continue the original task",
      source: callback.source,
    };
    const foreign = run(callback);
    if (!foreign.assistantPin) throw new Error("Missing assistant pin fixture");
    foreign.assistantPin.conversationRef = "cnv_foreign_fixture";
    const current = { ...conversation, turns: [callback] };
    const timeline = buildAssistantChatTimeline(
      current,
      current.turns,
      "org_owned_fixture",
      { [foreign.ref]: foreign },
      { [foreign.ref]: graph(callback) },
      [execution],
    );
    expect(timeline[0]?.kind).toBe("TURN");
    execution.message.source = { origin: "ORDINARY" };
    const item = buildRunTranscriptItems([execution])[0];
    expect(item?.summary).toBe("Continue the original task");
    expect(item?.kind).toBe("initiator");
    execution.message.source = callback.source;
    execution.actor.kind = "USER";
    expect(buildRunTranscriptItems([execution])[0]?.summary).toBeUndefined();
  });
});
const runs = Object.fromEntries(
  [first, second].map((anchor) => [anchor.runRef ?? "", run(anchor)]),
);
const graphs = Object.fromEntries(
  [first, second].map((anchor) => [anchor.runRef ?? "", graph(anchor)]),
);
const build = (
  events: RunEvent[],
  changedConversation = conversation,
  changedRuns = runs,
  changedGraphs = graphs,
) =>
  buildAssistantChatTimeline(
    changedConversation,
    changedConversation.turns,
    "org_owned_fixture",
    changedRuns,
    changedGraphs,
    events,
  );

describe("Единая лента receipts и exact runtime", () => {
  it("один активный индикатор принадлежит последнему exact событию, не каждому блоку или isolated history", () => {
    const earlier = event(first, 1),
      latest = event(second, 1);
    const foreign = { ...event(second, 2), execution: undefined };
    const timeline = build([earlier, latest, foreign]);
    expect(activeAssistantChatItemId(timeline, [])).toBe(
      buildRunTranscriptItems([latest])[0]?.id,
    );
    const keys = [
      executionKey(earlier.execution),
      executionKey(latest.execution),
    ].filter((key): key is string => Boolean(key));
    expect(activeAssistantChatItemId(timeline, keys)).toBeNull();
  });
  it("чередует server-ordered user/activity/receipt до следующего хода, не allactivity-first", () => {
    const events = [event(second, 1), event(first, 2), event(first, 1)];
    const original = JSON.stringify(events);
    const timeline = build(events);
    expect(timeline.map((item) => item.id)).toEqual([
      first.ref,
      `activity:${first.ref}`,
      final.ref,
      second.ref,
      `activity:${second.ref}`,
    ]);
    expect(
      timeline
        .filter((item) => item.kind === "ACTIVITY")
        .flatMap((item) => item.events),
    ).toHaveLength(3);
    expect(JSON.stringify(events)).toBe(original);
  });
  it("невидимый USER receipt сохраняет позицию exact опубликованного сообщения и плана", () => {
    const timeline = buildAssistantChatTimeline(
      conversation,
      [final, second],
      "org_owned_fixture",
      runs,
      graphs,
      [event(first, 1)],
    );
    expect(timeline.map((item) => item.id)).toEqual([
      `activity:${first.ref}`,
      final.ref,
      second.ref,
    ]);
  });
  it.each([
    "runRef",
    "nodeRef",
    "sessionRef",
    "turnRef",
    "turnNumber",
    "attempt",
  ] as const)("чужой %s не связывается с receipt и не подавляет его", (key) => {
    const original = event(first, 1);
    const changed: RunEvent = {
      ...original,
      execution: {
        ...original.execution,
        [key]:
          typeof original.execution?.[key] === "number" ? 9 : "foreign_fixture",
      } as NonNullable<RunEvent["execution"]>,
    };
    const timeline = build([changed]);
    expect(
      timeline.some((item) => item.kind === "ACTIVITY" && !item.isolated),
    ).toBe(false);
    expect(timeline.at(-1)).toMatchObject({
      kind: "ACTIVITY",
      isolated: true,
      events: [changed],
    });
    expect(timeline[0]).toMatchObject({ kind: "TURN", turn: first });
  });
  it("unknown/unscoped сохраняется отдельно, даже с совпавшим номером и временем", () => {
    const unknown = { ...event(first, 1), execution: undefined };
    expect(build([unknown]).at(-1)).toMatchObject({
      isolated: true,
      events: [unknown],
    });
  });
  it.each([
    "organizationRef",
    "conversationRef",
    "assistantRef",
    "scope",
  ] as const)("недоказанный owner pin %s не объединяется", (key) => {
    const current = run(first);
    const changed = {
      ...current,
      assistantPin: { ...current.assistantPin, [key]: "foreign_fixture" },
    } as Run;
    expect(
      build([event(first, 1)], conversation, {
        ...runs,
        [current.ref]: changed,
      }).at(-1),
    ).toMatchObject({ isolated: true });
  });
  it("PROJECT связывается только со своим project/profile tuple", () => {
    const project = {
      ...conversation,
      assistantScope: "PROJECT" as const,
      projectRef: "prj_owned_fixture",
      assistantProfileRef: "asstp_owned_fixture",
    };
    const current = run(first);
    const owned = {
      ...current,
      projectRef: project.projectRef,
      assistantPin: {
        ...current.assistantPin,
        scope: "PROJECT" as const,
        organizationRef: "org_owned_fixture",
        conversationRef: project.ref,
        assistantRef: project.assistantRef,
        projectRef: project.projectRef,
        profileRef: project.assistantProfileRef,
      },
    };
    expect(
      build([event(first, 1)], project, { ...runs, [current.ref]: owned }).some(
        (item) => item.kind === "ACTIVITY" && !item.isolated,
      ),
    ).toBe(true);
    expect(
      build(
        [event(first, 1)],
        { ...project, assistantProfileRef: "asstp_foreign_fixture" },
        { ...runs, [current.ref]: owned },
      ).at(-1),
    ).toMatchObject({ isolated: true });
  });
  it("ambiguous USER anchors и чужой graph не дают временной привязки", () => {
    expect(
      build([event(first, 1)], {
        ...conversation,
        turns: [
          ...conversation.turns,
          { ...first, ref: "trn_duplicate_fixture" },
        ],
      }).at(-1),
    ).toMatchObject({ isolated: true });
    expect(
      build([event(first, 1)], conversation, runs, {
        ...graphs,
        [first.runRef ?? ""]: {
          ...graph(first),
          runRef: "run_foreign_fixture",
        },
      }).at(-1),
    ).toMatchObject({ isolated: true });
  });

  it("retry использует exact current attempt в owner root graph, не прежнюю попытку", () => {
    const current = {
      ...run(first),
      attempt: 2,
      rootRunRef: "run_root_fixture",
    };
    const retryGraph = {
      ...graph(first),
      runRef: current.rootRunRef,
      nodes: graph(first).nodes.map((node) => ({ ...node, attempt: 2 })),
    };
    const original = event(first, 1);
    if (!original.execution) throw new Error("Missing synthetic execution");
    const retry = {
      ...original,
      ref: "evt_retry_fixture",
      execution: { ...original.execution, attempt: 2 },
    };
    const timeline = build(
      [original, retry],
      conversation,
      { ...runs, [current.ref]: current },
      { [current.rootRunRef]: retryGraph },
    );
    expect(
      timeline.find((item) => item.kind === "ACTIVITY" && !item.isolated),
    ).toMatchObject({ events: [retry] });
    expect(timeline.at(-1)).toMatchObject({
      isolated: true,
      events: [original],
    });
  });
});
