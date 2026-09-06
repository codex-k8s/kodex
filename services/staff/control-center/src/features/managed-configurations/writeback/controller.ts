import type {
  ConfigurationWriteBackView,
  ManagedConfiguration,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { idempotencyKey } from "@/shared/api/mutation";
import {
  actionReason,
  clearIntent,
  clearRejectedPreparation,
  contentDigest,
  loadIntent,
  pollsNeeded,
  preparationReason,
  saveIntent,
  matchesPreparation,
  markRejectedPreparation,
  rejectedPreparation,
  type Action,
  type Intent,
  type Proposal,
  type RecoveryStorage,
} from "./model";
import * as api from "./api";

// Экземпляр принадлежит одному открытому configuration и одному owner lifetime.
// В хранилище остаются только metadata намерения, документы живут в памяти.
export class WriteBackController {
  view?: ConfigurationWriteBackView;
  items: Proposal[] = [];
  total = 0;
  loaded = false;
  cursor?: string;
  content = "";
  pending?: Intent;
  rejected = false;
  problem?: AppProblem;
  working = false;
  paused = false;
  stale = false;
  polls = 0;
  private readonly abort = new AbortController();
  readonly signal: AbortSignal;
  constructor(
    public configuration: ManagedConfiguration,
    private readonly storage: RecoveryStorage,
    ownerSignal: AbortSignal,
    private readonly port: Pick<
      typeof api,
      "executeIntent" | "readProposal" | "listProposals"
    > = api,
  ) {
    this.signal = AbortSignal.any([this.abort.signal, ownerSignal]);
    try {
      this.pending = loadIntent(storage, configuration.ref);
      this.rejected =
        !!this.pending && rejectedPreparation(storage, this.pending);
    } catch (error) {
      this.problem = asProblem(error);
      this.stale = true;
    }
  }
  close(): void {
    this.abort.abort();
    this.content = "";
    this.view = undefined;
    this.items = [];
  }
  revoke(): void {
    if (this.pending) {
      try {
        clearIntent(this.storage, this.pending);
      } catch {
        /* Повреждённое хранилище не даёт продолжить mutation. */
      }
    }
    this.pending = undefined;
    this.close();
  }
  update(configuration: ManagedConfiguration): void {
    if (configuration.ref !== this.configuration.ref)
      throw new Error("Write-back controller scope changed");
    this.configuration = configuration;
  }
  async history(more = false): Promise<void> {
    await this.run(async () => {
      const page = await this.port.listProposals(
        this.configuration.ref,
        this.signal,
        more ? this.cursor : undefined,
        more ? this.items : [],
      );
      this.signal.throwIfAborted();
      this.items = more ? [...this.items, ...page.items] : page.items;
      this.cursor = page.nextPageToken;
      this.total = page.total;
      this.loaded = true;
      if (!this.view && !this.pending) this.stale = false;
    });
  }
  async select(proposal: Proposal): Promise<void> {
    if (
      this.pending &&
      this.pending.action !== "PREPARE" &&
      this.pending.proposalRef !== proposal.ref
    )
      return;
    await this.run(async () => {
      this.view = undefined;
      const view = await this.port.readProposal(
        this.configuration.ref,
        proposal.ref,
        this.signal,
        proposal,
      );
      this.signal.throwIfAborted();
      this.view = view;
      this.stale = false;
      this.polls = 0;
      this.paused = false;
    });
  }
  async prepare(): Promise<void> {
    if (this.pending && this.pending.action !== "PREPARE") return;
    const originallyPending = !!this.pending;
    await this.run(async () => {
      let intent = this.pending;
      if (!intent) {
        if (preparationReason(this.configuration))
          throw new Error("Write-back source is unavailable");
        const contentHash = await contentDigest(this.content);
        this.signal.throwIfAborted();
        const source = this.configuration.gitSource;
        if (!source) throw new Error("Write-back source is unavailable");
        intent = {
          action: "PREPARE",
          kind: this.configuration.kind as Intent["kind"],
          configurationRef: this.configuration.ref,
          version: this.configuration.version,
          sourceVersion: source.version,
          sourceRef: source.ref,
          contentDigest: contentHash,
          key: idempotencyKey(),
        };
        saveIntent(this.storage, intent);
        this.pending = intent;
      }
      const knownBeforeAttempt = !originallyPending || this.rejected;
      if (this.rejected) {
        clearRejectedPreparation(this.storage, intent);
        this.rejected = false;
      }
      let proposal: Proposal;
      try {
        proposal = await this.port.executeIntent(
          intent,
          this.signal,
          this.content,
        );
      } catch (error) {
        if (
          error instanceof api.PreparationRejected &&
          knownBeforeAttempt &&
          !this.signal.aborted
        ) {
          markRejectedPreparation(this.storage, intent);
          this.rejected = true;
        }
        throw error;
      }
      this.signal.throwIfAborted();
      // Если Get не подтвердил документы, approval недоступен. Ref сохраняется в
      // памяти, а Prepare повторяется с прежним ключом после reload.
      const view = await this.port.readProposal(
        this.configuration.ref,
        proposal.ref,
        this.signal,
        proposal,
      );
      this.signal.throwIfAborted();
      clearIntent(this.storage, intent);
      this.pending = undefined;
      this.rejected = false;
      this.content = "";
      this.view = view;
      this.stale = false;
    });
  }
  async decide(action: Action): Promise<void> {
    if (this.pending && this.pending.action !== action) return;
    await this.run(async () => {
      let intent = this.pending;
      if (!intent) {
        const proposal = this.view?.proposal;
        if (!proposal || this.stale || actionReason(proposal, action))
          throw new Error("Write-back action is unavailable");
        intent = {
          action,
          kind: proposal.kind,
          configurationRef: proposal.configurationRef,
          proposalRef: proposal.ref,
          version: proposal.version,
          approvalDigest: proposal.approvalDigest,
          key: idempotencyKey(),
        };
        saveIntent(this.storage, intent);
        this.pending = intent;
      }
      const proposal = await this.port.executeIntent(intent, this.signal);
      this.signal.throwIfAborted();
      const view = await this.port.readProposal(
        this.configuration.ref,
        proposal.ref,
        this.signal,
        this.view?.proposal ?? proposal,
      );
      this.signal.throwIfAborted();
      clearIntent(this.storage, intent);
      this.pending = undefined;
      this.view = view;
      this.stale = false;
      this.polls = 0;
      this.paused = false;
    });
  }
  discardRejected(): void {
    if (!this.pending || !this.rejected || this.working || this.signal.aborted)
      return;
    clearIntent(this.storage, this.pending);
    this.pending = undefined;
    this.rejected = false;
    this.stale = false;
    this.problem = undefined;
  }
  async adoptPrepared(): Promise<void> {
    const intent = this.pending;
    const proposal = this.view?.proposal;
    if (!intent || !proposal || !matchesPreparation(intent, proposal)) return;
    await this.run(async () => {
      const view = await this.port.readProposal(
        this.configuration.ref,
        proposal.ref,
        this.signal,
        proposal,
      );
      this.signal.throwIfAborted();
      if (!matchesPreparation(intent, view.proposal))
        throw new Error("Write-back history plan does not match preparation");
      clearIntent(this.storage, intent);
      this.pending = undefined;
      this.rejected = false;
      this.view = view;
      this.content = "";
      this.stale = false;
    });
  }
  async recover(): Promise<void> {
    const intent = this.pending;
    const ref = intent?.proposalRef ?? this.view?.proposal.ref;
    if (!ref) {
      await this.history();
      return;
    }
    await this.run(async () => {
      const view = await this.port.readProposal(
        this.configuration.ref,
        ref,
        this.signal,
        this.view?.proposal,
      );
      this.signal.throwIfAborted();
      this.view = view;
      this.stale = false;
      if (intent && view.proposal.version > intent.version) {
        // Чтение подтверждает текущее состояние, а не приписывает нам чужой эффект.
        clearIntent(this.storage, intent);
        this.pending = undefined;
      }
    });
  }
  async poll(): Promise<void> {
    if (
      this.working ||
      this.pending ||
      !this.view ||
      !pollsNeeded(this.view.proposal) ||
      this.signal.aborted
    )
      return;
    if (this.polls >= 100) {
      this.paused = true;
      return;
    }
    this.polls += 1;
    await this.recover();
    if (this.problem) this.paused = true;
  }
  private async run(operation: () => Promise<void>): Promise<void> {
    if (this.working || this.signal.aborted) return;
    this.working = true;
    this.problem = undefined;
    try {
      await operation();
    } catch (error) {
      if (this.active()) {
        this.problem = asProblem(error);
        this.stale = true;
        if ([401, 403, 404].includes(this.problem.status)) {
          this.content = "";
          this.view = undefined;
          this.items = [];
          this.total = 0;
          this.cursor = undefined;
        }
      }
    } finally {
      this.working = false;
    }
  }
  private active(): boolean {
    return !this.signal.aborted;
  }
}
