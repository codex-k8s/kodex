import { defineStore } from "pinia";

import { asProblem, type AppProblem } from "@/shared/api/problem";
import { KnownMutationRejection } from "@/shared/api/mutation-rejection";

import {
  authorizeProviderApiKey,
  createProviderAccount,
  loadProviderAccount,
  loadProviderAccounts,
  loadProviderDefinitions,
  revokeProviderAccount,
  setProviderAccountEnabled,
  startDeviceAuthorization,
} from "./api";
import {
  accountAllows,
  isPendingDeviceAuthorization,
  pageAllowsAccountCreation,
  upsertProviderAccount,
  type ProviderAccount,
  type ProviderAccountCreateInput,
  type ProviderAccountAction,
  type ProviderDefinition,
} from "./model";
import { startProviderLifecycle } from "./lifecycle";
import { requestSignal } from "@/shared/api/client";

const devicePollDelayMs = 4_000;
const pollTimers = new Map<string, ReturnType<typeof setTimeout>>();
const pollGenerations = new Map<string, number>();
let loadGeneration = 0;
let loadController: AbortController | undefined;

interface ProvidersState {
  definitions: ProviderDefinition[];
  definitionsNextPageToken: string;
  definitionsLoadingMore: boolean;
  accounts: ProviderAccount[];
  accountsNextPageToken: string;
  pageNextActions: ProviderAccountAction[];
  query: string;
  loading: boolean;
  loadingMore: boolean;
  busyRefs: string[];
  pollingRefs: string[];
  problem?: AppProblem;
}

export const useProvidersStore = defineStore("providers", {
  state: (): ProvidersState => ({
    definitions: [],
    definitionsNextPageToken: "",
    definitionsLoadingMore: false,
    accounts: [],
    accountsNextPageToken: "",
    pageNextActions: [],
    query: "",
    loading: false,
    loadingMore: false,
    busyRefs: [],
    pollingRefs: [],
    problem: undefined,
  }),
  actions: {
    async loadDefinitions(query = ""): Promise<void> {
      const page = await loadProviderDefinitions(query);
      this.definitions = page.items;
      this.definitionsNextPageToken = page.nextPageToken;
    },
    async loadMoreDefinitions(): Promise<void> {
      if (!this.definitionsNextPageToken || this.definitionsLoadingMore) return;
      this.definitionsLoadingMore = true;
      this.problem = undefined;
      try {
        const page = await loadProviderDefinitions(
          "",
          this.definitionsNextPageToken,
        );
        const definitions = new Map(
          this.definitions.map((definition) => [definition.key, definition]),
        );
        for (const definition of page.items)
          definitions.set(definition.key, definition);
        this.definitions = [...definitions.values()].sort((left, right) =>
          left.name.localeCompare(right.name, "ru"),
        );
        this.definitionsNextPageToken = page.nextPageToken;
      } catch (error) {
        this.problem = asProblem(error);
      } finally {
        this.definitionsLoadingMore = false;
      }
    },
    async load(query?: string): Promise<void> {
      this.query = (query ?? this.query).trim();
      this.loading = true;
      this.problem = undefined;
      loadController?.abort();
      const controller = new AbortController();
      loadController = controller;
      const generation = ++loadGeneration;
      try {
        const [definitions, accounts] = await Promise.all([
          loadProviderDefinitions("", undefined, controller.signal),
          loadProviderAccounts(this.query, undefined, controller.signal),
        ]);
        if (controller.signal.aborted || generation !== loadGeneration) return;
        this.definitions = definitions.items;
        this.definitionsNextPageToken = definitions.nextPageToken;
        this.accounts = accounts.items;
        this.accountsNextPageToken = accounts.nextPageToken;
        this.pageNextActions = accounts.nextActions;
      } catch (error) {
        if (controller.signal.aborted || generation !== loadGeneration) return;
        this.problem = asProblem(error);
      } finally {
        if (generation === loadGeneration) this.loading = false;
        if (loadController === controller) loadController = undefined;
      }
    },
    async loadMore(): Promise<void> {
      if (!this.accountsNextPageToken || this.loadingMore) return;
      this.loadingMore = true;
      this.problem = undefined;
      const token = this.accountsNextPageToken;
      const generation = loadGeneration;
      const query = this.query;
      try {
        const page = await loadProviderAccounts(query, token);
        if (generation !== loadGeneration || query !== this.query) return;
        for (const account of page.items)
          this.accounts = upsertProviderAccount(this.accounts, account);
        this.accountsNextPageToken = page.nextPageToken;
        this.pageNextActions = page.nextActions;
      } catch (error) {
        if (generation === loadGeneration && query === this.query)
          this.problem = asProblem(error);
      } finally {
        this.loadingMore = false;
      }
    },
    async create(input: ProviderAccountCreateInput): Promise<ProviderAccount> {
      if (!pageAllowsAccountCreation(this.pageNextActions))
        throw new Error("Provider account creation is unavailable");
      return this.execute("create", () => createProviderAccount(input));
    },
    async startDevice(
      account: ProviderAccount,
      reauthorize = false,
    ): Promise<ProviderAccount> {
      if (!accountAllows(account, "CONFIGURE_CREDENTIAL")) return account;
      const generation = pollGenerations.get(account.ref) ?? 0;
      const updated = await this.execute(account.ref, () =>
        reauthorize || account.state === "REAUTHORIZATION_REQUIRED"
          ? startProviderLifecycle(
              account,
              { action: "REAUTHORIZE" },
              window.sessionStorage,
              requestSignal(),
            ).then((result) => result.account)
          : startDeviceAuthorization(account),
      );
      if (
        generation === (pollGenerations.get(account.ref) ?? 0) &&
        isPendingDeviceAuthorization(updated)
      )
        this.schedulePoll(updated.ref);
      return updated;
    },
    async refreshAuthorization(
      account: ProviderAccount,
    ): Promise<ProviderAccount> {
      if (!accountAllows(account, "REFRESH_AUTHORIZATION")) {
        this.stopPolling(account.ref);
        return account;
      }
      const generation = pollGenerations.get(account.ref) ?? 0;
      const updated = await this.execute(account.ref, () =>
        startProviderLifecycle(
          account,
          { action: "VERIFY" },
          window.sessionStorage,
          requestSignal(),
        ).then((result) => result.account),
      );
      if (generation === (pollGenerations.get(account.ref) ?? 0)) {
        if (isPendingDeviceAuthorization(updated))
          this.schedulePoll(updated.ref);
        else this.stopPolling(updated.ref);
      }
      return updated;
    },
    async authorizeApiKey(
      account: ProviderAccount,
      apiKey: string,
    ): Promise<ProviderAccount> {
      if (!accountAllows(account, "CONFIGURE_CREDENTIAL")) return account;
      return this.execute(account.ref, () =>
        authorizeProviderApiKey(account, apiKey),
      );
    },
    async setEnabled(
      account: ProviderAccount,
      enabled: boolean,
    ): Promise<ProviderAccount> {
      const action: ProviderAccountAction = enabled ? "ENABLE" : "DISABLE";
      if (!accountAllows(account, action)) return account;
      return this.execute(account.ref, () =>
        setProviderAccountEnabled(account, enabled),
      );
    },
    async revoke(account: ProviderAccount): Promise<ProviderAccount> {
      if (!accountAllows(account, "REVOKE")) return account;
      this.stopPolling(account.ref);
      return this.execute(account.ref, () => revokeProviderAccount(account));
    },
    async remove(account: ProviderAccount): Promise<ProviderAccount> {
      if (!accountAllows(account, "DELETE")) return account;
      this.stopPolling(account.ref);
      return this.execute(account.ref, () =>
        startProviderLifecycle(
          account,
          { action: "DELETE" },
          window.sessionStorage,
          requestSignal(),
        ).then((result) => result.account),
      );
    },
    schedulePoll(accountRef: string): void {
      this.stopPolling(accountRef);
      this.pollingRefs = [...new Set([...this.pollingRefs, accountRef])];
      pollTimers.set(
        accountRef,
        setTimeout(() => {
          pollTimers.delete(accountRef);
          const account = this.accounts.find((item) => item.ref === accountRef);
          if (!account || !isPendingDeviceAuthorization(account)) {
            this.stopPolling(accountRef);
            return;
          }
          void this.refreshAuthorization(account).catch(() => {
            this.stopPolling(accountRef);
          });
        }, devicePollDelayMs),
      );
    },
    stopPolling(accountRef: string): void {
      pollGenerations.set(
        accountRef,
        (pollGenerations.get(accountRef) ?? 0) + 1,
      );
      const timer = pollTimers.get(accountRef);
      if (timer !== undefined) clearTimeout(timer);
      pollTimers.delete(accountRef);
      this.pollingRefs = this.pollingRefs.filter((ref) => ref !== accountRef);
    },
    stopAllPolling(): void {
      for (const accountRef of new Set([...this.pollingRefs, ...this.busyRefs]))
        this.stopPolling(accountRef);
    },
    async execute(
      busyRef: string,
      operation: () => Promise<ProviderAccount>,
    ): Promise<ProviderAccount> {
      if (this.busyRefs.includes(busyRef))
        throw new Error("Provider account mutation is already in progress");
      this.busyRefs = [...new Set([...this.busyRefs, busyRef])];
      this.problem = undefined;
      try {
        const account = await operation();
        this.accounts = upsertProviderAccount(this.accounts, account);
        return account;
      } catch (error) {
        if (error instanceof KnownMutationRejection && error.status === 412) {
          const signal = requestSignal();
          try {
            const current = await loadProviderAccount(busyRef, signal);
            signal.throwIfAborted();
            if (current.ref !== busyRef)
              throw new Error("Provider refresh scope changed");
            this.accounts = upsertProviderAccount(this.accounts, current);
          } catch (refreshError) {
            if (!signal.aborted) {
              this.accounts = this.accounts.filter(
                (item) => item.ref !== busyRef,
              );
              this.problem = asProblem(refreshError);
            }
            throw refreshError;
          }
        }
        this.problem = asProblem(error);
        throw this.problem;
      } finally {
        this.busyRefs = this.busyRefs.filter((ref) => ref !== busyRef);
      }
    },
  },
});
