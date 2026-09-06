import { expect, it } from "vitest";
import {
  actionReason,
  checkedIntent,
  checkedPage,
  checkedProposal,
  checkedView,
  clearIntent,
  contentDigest,
  loadIntent,
  pollsNeeded,
  preparationReason,
  safePullRequestUrl,
  saveIntent,
  type Intent,
} from "./model";
import { memoryStorage, writeBackFixture } from "./fixtures";

it("проверяет точные raw bytes diff и immutable lineage без нормализации YAML", async () => {
  const { configuration, view, proposal } = await writeBackFixture();
  expect(await checkedView(view, configuration.ref)).toEqual(view);
  await expect(
    checkedView({ ...view, baseContent: "key: old" }, configuration.ref),
  ).rejects.toThrow("digest");
  await expect(
    checkedView(
      { ...view, proposedContent: "key: tampered\n" },
      configuration.ref,
    ),
  ).rejects.toThrow("digest");
  for (const change of [
    { sourceVersion: 5 },
    { approvalDigest: "c".repeat(64) },
    { baseCommitSha: "d".repeat(40) },
    { version: 0 },
    { configurationRef: "foreign" },
    { proposalBranch: "main" },
  ])
    expect(() =>
      checkedProposal({ ...proposal, ...change }, configuration.ref, proposal),
    ).toThrow();
});
it("различает terminal PR, неизвестный исход и серверные disabled actions", async () => {
  const { proposal, configuration } = await writeBackFixture();
  if (!configuration.gitSource) throw new Error("fixture source missing");
  expect(preparationReason(configuration)).toBeUndefined();
  expect(preparationReason({ ...configuration, managedBy: "UI" })).toBe("git");
  expect(
    preparationReason({
      ...configuration,
      gitSource: { ...configuration.gitSource, state: "CLAIMED" },
    }),
  ).toBe("source");
  expect(actionReason(proposal, "APPROVE")).toBeUndefined();
  expect(
    actionReason({ ...proposal, expiresAt: "2000-01-01T00:00:00Z" }, "APPROVE"),
  ).toBe("EXPIRED");
  expect(
    actionReason(
      {
        ...proposal,
        nextActions: [
          { action: "APPROVE", enabled: false, reason: "FORBIDDEN" },
          proposal.nextActions[1],
          proposal.nextActions[2],
        ],
      },
      "APPROVE",
    ),
  ).toBe("FORBIDDEN");
  expect(pollsNeeded({ ...proposal, state: "UNKNOWN_OUTCOME" })).toBe(true);
  expect(pollsNeeded({ ...proposal, state: "SUCCEEDED" })).toBe(false);
  expect(() =>
    checkedProposal({ ...proposal, state: "SUCCEEDED" }, configuration.ref),
  ).toThrow();
  expect(safePullRequestUrl("javascript:alert(1)")).toBeUndefined();
  expect(safePullRequestUrl("https://token@example.com/pr/1")).toBeUndefined();
});
it("history принимает owner total и отклоняет повтор cursor/строк и чужой scope", async () => {
  const { proposal, configuration } = await writeBackFixture();
  const page = { items: [proposal], total: 95, nextPageToken: "next" };
  expect(checkedPage(page, configuration.ref).total).toBe(95);
  expect(() => checkedPage(page, configuration.ref, "next")).toThrow();
  expect(() =>
    checkedPage(page, configuration.ref, undefined, [proposal]),
  ).toThrow();
  expect(() => checkedPage(page, "foreign")).toThrow();
});
it("recovery сохраняет лишь metadata, первоначальный OCC/ключ и закрыто проверяет повреждения", async () => {
  const { configuration, proposal, view } = await writeBackFixture();
  const storage = memoryStorage();
  const intent: Intent = {
    action: "PREPARE",
    configurationRef: configuration.ref,
    kind: "ROLE_IMAGE",
    version: 8,
    sourceRef: proposal.sourceRef,
    sourceVersion: 4,
    contentDigest: proposal.proposedContentSha256,
    key: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  };
  saveIntent(storage, intent);
  expect(loadIntent(storage, configuration.ref)).toEqual(intent);
  expect([...storage.data.values()][0]).not.toContain(view.proposedContent);
  expect(() => saveIntent(storage, { ...intent, version: 9 })).toThrow();
  expect(() =>
    checkedIntent({ ...intent, content: "private" } as Intent),
  ).toThrow();
  clearIntent(storage, {
    ...intent,
    key: "ffffffff-bbbb-cccc-dddd-eeeeeeeeeeee",
  });
  expect(loadIntent(storage, configuration.ref)).toEqual(intent);
  clearIntent(storage, intent);
  expect(loadIntent(storage, configuration.ref)).toBeUndefined();
  await expect(contentDigest("я".repeat(131073))).rejects.toThrow();
});
