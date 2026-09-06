import { expect, it, vi } from "vitest";
import { WriteBackController } from "./controller";
import { PreparationRejected } from "./api";
import { AppProblem } from "@/shared/api/problem";
import { loadIntent, type Proposal } from "./model";
import { memoryStorage, writeBackFixture } from "./fixtures";

async function fixture() {
  const data = await writeBackFixture();
  const port = {
    executeIntent: vi.fn().mockResolvedValue(data.proposal),
    readProposal: vi.fn().mockResolvedValue(data.view),
    listProposals: vi
      .fn()
      .mockResolvedValue({ items: [data.proposal], total: 1 }),
  };
  const storage = memoryStorage();
  const owner = new AbortController();
  return {
    ...data,
    port,
    storage,
    owner,
    state: new WriteBackController(
      data.configuration,
      storage,
      owner.signal,
      port,
    ),
  };
}
it("Prepare не подтверждает план автоматически и не сохраняет документ", async () => {
  const { state, view, storage, configuration, port } = await fixture();
  state.content = view.proposedContent;
  await state.prepare();
  expect(state.view).toEqual(view);
  expect(state.pending).toBeUndefined();
  expect(storage.data.size).toBe(0);
  expect(port.executeIntent).toHaveBeenCalledTimes(1);
  expect(port.executeIntent.mock.calls[0]?.[0]).toMatchObject({
    action: "PREPARE",
    version: configuration.version,
    sourceVersion: 4,
  });
});
it("неизвестный Prepare переживает reload и повторяет исходный intent, а не новый OCC", async () => {
  const { state, view, storage, configuration, port, owner } = await fixture();
  port.executeIntent.mockRejectedValueOnce(new Error("network"));
  state.content = view.proposedContent;
  await state.prepare();
  const intent = state.pending;
  expect(intent).toBeDefined();
  state.close();
  const restored = new WriteBackController(
    { ...configuration, version: 9 },
    storage,
    owner.signal,
    port,
  );
  expect(restored.content).toBe("");
  restored.content = view.proposedContent;
  await restored.prepare();
  expect(port.executeIntent.mock.calls[1]?.[0]).toEqual(intent);
  expect(loadIntent(storage, configuration.ref)).toBeUndefined();
});
it("unknown decision восстанавливается через Get и не повторяет effect", async () => {
  const { state, view, port } = await fixture();
  state.view = view;
  port.executeIntent.mockRejectedValueOnce(new Error("lost ACK"));
  await state.decide("APPROVE");
  expect(state.pending?.action).toBe("APPROVE");
  const progressed = {
    ...view,
    proposal: {
      ...view.proposal,
      version: 2,
      state: "UNKNOWN_OUTCOME" as const,
    },
  };
  port.readProposal.mockResolvedValueOnce(progressed);
  await state.recover();
  expect(state.pending).toBeUndefined();
  expect(state.view.proposal.state).toBe("UNKNOWN_OUTCOME");
  expect(port.executeIntent).toHaveBeenCalledTimes(1);
});
it("current version без нового receipt сохраняет pending и первоначальный retry tuple", async () => {
  const { state, view, port } = await fixture();
  state.view = view;
  port.executeIntent.mockRejectedValue(new Error("network"));
  await state.decide("REJECT");
  const intent = state.pending;
  await state.recover();
  expect(state.pending).toEqual(intent);
  await state.decide("CANCEL");
  expect(port.executeIntent).toHaveBeenCalledTimes(1);
  await state.decide("REJECT");
  expect(port.executeIntent.mock.calls[1]?.[0]).toEqual(intent);
});
it("отзыв прав скрывает документы и запрещает старую approval", async () => {
  const { state, view, port } = await fixture();
  state.view = view;
  port.readProposal.mockRejectedValue(
    new AppProblem({
      status: 403,
      kind: "forbidden",
      code: "FORBIDDEN",
      retryable: false,
    }),
  );
  await state.recover();
  expect(state.view).toBeUndefined();
  expect(state.stale).toBe(true);
  await state.decide("APPROVE");
  expect(port.executeIntent).not.toHaveBeenCalled();
});
it("смена owner либо configuration lifetime отклоняет запоздалый Get/ACK", async () => {
  for (const revoke of [false, true]) {
    const { state, view, port, owner } = await fixture();
    let finish!: (value: typeof view) => void;
    port.readProposal.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const operation = state.select(view.proposal);
    if (revoke) owner.abort();
    else state.close();
    finish(view);
    await operation;
    expect(state.view).toBeUndefined();
  }
});
it("bounded polling только читает UNKNOWN и останавливается после100 checks", async () => {
  const { state, view, port } = await fixture();
  const proposal: Proposal = { ...view.proposal, state: "UNKNOWN_OUTCOME" };
  state.view = { ...view, proposal };
  port.readProposal.mockResolvedValue({ ...view, proposal });
  for (let index = 0; index < 102; index++) await state.poll();
  expect(port.readProposal).toHaveBeenCalledTimes(100);
  expect(port.executeIntent).not.toHaveBeenCalled();
  expect(state.paused).toBe(true);
});
it("новая source revision не меняет сохраняемое намерение при lost ACK", async () => {
  const { state, view, configuration, port } = await fixture();
  state.content = view.proposedContent;
  port.executeIntent.mockRejectedValueOnce(new Error("lost ACK"));
  await state.prepare();
  const original = state.pending;
  if (!configuration.gitSource) throw new Error("fixture source missing");
  state.update({
    ...configuration,
    version: 11,
    gitSource: { ...configuration.gitSource, version: 7, state: "CLAIMED" },
  });
  await state.prepare();
  expect(port.executeIntent.mock.calls[1]?.[0]).toEqual(original);
});
it("ошибка Get после mutation не выдаётся за показанный и подтверждённый diff", async () => {
  const { state, view, port } = await fixture();
  state.content = view.proposedContent;
  port.readProposal.mockRejectedValue(new Error("read unavailable"));
  await state.prepare();
  expect(state.view).toBeUndefined();
  expect(state.pending?.action).toBe("PREPARE");
  await state.decide("APPROVE");
  expect(port.executeIntent).toHaveBeenCalledTimes(1);
});
it("pending Prepare допускает явное принятие exact history plan без заявления исходного receipt", async () => {
  const { state, view, port } = await fixture();
  state.content = view.proposedContent;
  port.executeIntent.mockRejectedValue(new Error("lost ACK"));
  await state.prepare();
  await state.select(view.proposal);
  expect(state.pending).toBeDefined();
  await state.adoptPrepared();
  expect(state.pending).toBeUndefined();
  expect(state.view).toEqual(view);
  expect(port.executeIntent).toHaveBeenCalledTimes(1);
});
it("history plan с другим source pin не снимает unknown preparation", async () => {
  const { state, view, port } = await fixture();
  state.content = view.proposedContent;
  port.executeIntent.mockRejectedValue(new Error("lost ACK"));
  await state.prepare();
  state.view = { ...view, proposal: { ...view.proposal, sourceVersion: 5 } };
  await state.adoptPrepared();
  expect(state.pending).toBeDefined();
  expect(port.readProposal).not.toHaveBeenCalled();
});
it("сброс доступен после typed окончательного rejection, но не proxy/lost ACK", async () => {
  for (const error of [
    new PreparationRejected(400),
    new PreparationRejected(412),
    new Error("proxy"),
    new AppProblem({
      status: 400,
      kind: "unknown",
      code: "UNKNOWN",
      retryable: false,
    }),
  ]) {
    const { state, view, port, storage, configuration, owner } =
      await fixture();
    state.content = view.proposedContent;
    port.executeIntent.mockRejectedValue(error);
    await state.prepare();
    const restored = new WriteBackController(
      configuration,
      storage,
      owner.signal,
      port,
    );
    expect(restored.rejected).toBe(error instanceof PreparationRejected);
    restored.discardRejected();
    expect(!!restored.pending).toBe(!(error instanceof PreparationRejected));
    expect(port.executeIntent).toHaveBeenCalledTimes(1);
  }
});
it.each([400, 412] as const)(
  "typed rejection %s после уже неизвестного запроса не отменяет его ambiguity",
  async (status) => {
    const { state, view, port } = await fixture();
    state.content = view.proposedContent;
    port.executeIntent.mockRejectedValueOnce(new Error("lost ACK"));
    await state.prepare();
    port.executeIntent.mockRejectedValueOnce(new PreparationRejected(status));
    await state.prepare();
    state.discardRejected();
    expect(state.pending).toBeDefined();
  },
);
it("после отклонённого PREPARE и обновления source создаёт новый key/OCC", async () => {
  const { state, view, port, configuration } = await fixture();
  state.content = view.proposedContent;
  port.executeIntent.mockRejectedValueOnce(new PreparationRejected(412));
  await state.prepare();
  const original = state.pending;
  expect(original).toBeDefined();
  state.discardRejected();
  if (!configuration.gitSource) throw new Error("Missing synthetic Git source");
  state.update({
    ...configuration,
    version: 9,
    gitSource: { ...configuration.gitSource, version: 5 },
  });
  port.executeIntent.mockRejectedValueOnce(new Error("lost ACK"));
  await state.prepare();
  expect(state.pending).toMatchObject({ version: 9, sourceVersion: 5 });
  expect(state.pending?.key).not.toBe(original?.key);
  expect(port.executeIntent).toHaveBeenCalledTimes(2);
});
it("повтор после известного отказа с lost ACK снимает прежний rejection marker до отправки", async () => {
  const { state, view, port, configuration, storage, owner } = await fixture();
  state.content = view.proposedContent;
  port.executeIntent.mockRejectedValueOnce(new PreparationRejected(400));
  await state.prepare();
  expect(state.rejected).toBe(true);
  const original = state.pending;
  port.executeIntent.mockRejectedValueOnce(new Error("lost ACK"));
  await state.prepare();
  const restored = new WriteBackController(
    configuration,
    storage,
    owner.signal,
    port,
  );
  expect(restored.rejected).toBe(false);
  restored.discardRejected();
  expect(restored.pending).toEqual(original);
});
