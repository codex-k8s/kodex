<script setup lang="ts">
import {
  Check,
  CircleAlert,
  Copy,
  Info,
  KeyRound,
  LoaderCircle,
  Plus,
  Power,
  PowerOff,
  RefreshCw,
  Search,
  ShieldOff,
  Smartphone,
  Trash2,
  Maximize2,
} from "@lucide/vue";
import { storeToRefs } from "pinia";
import { useI18n } from "vue-i18n";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  useId,
  watch,
} from "vue";

import { asProblem, type AppProblem } from "@/shared/api/problem";
import { readSpeechAvailability } from "@/shared/api/speech";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import { loadProviderAccount, loadProviderDefinitions } from "./api";
import ProviderUsageDetails from "./ProviderUsageDetails.vue";
import ProviderAccountLifecyclePanel from "./ProviderAccountLifecyclePanel.vue";
import ProviderLifecycleRecovery from "./ProviderLifecycleRecovery.vue";
import type { ProviderLifecycleResult } from "./lifecycle";

import {
  accountAllows,
  hasPendingDeviceAuthorization,
  isPendingDeviceAuthorization,
  pageAllowsAccountCreation,
  readableProviderBlocker,
  safeVerificationUri,
  upsertProviderAccount,
  type ProviderAccount,
  type ProviderAuthorizationMethod,
  type ProviderDefinitionKey,
} from "./model";
import { useProvidersStore } from "./store";

const store = useProvidersStore();
const { t } = useI18n();
const {
  accounts,
  accountsNextPageToken,
  busyRefs,
  definitions,
  definitionsLoadingMore,
  definitionsNextPageToken,
  loading,
  loadingMore,
  pageNextActions,
  pollingRefs,
  problem,
} = storeToRefs(store);
const search = ref("");
const searchId = useId();
const expandedSearchId = useId();
const createNameId = useId();
const apiKeyId = useId();
const expanded = ref(false);
const createOpen = ref(false);
const authorizationAccount = ref<ProviderAccount>();
const revokeAccount = ref<ProviderAccount>();
const impactAccount = ref<ProviderAccount>();
const authorizationRecoveryPending = ref(false);
const authorizationMethod = ref<ProviderAuthorizationMethod>("DEVICE_CODE");
const apiKey = ref("");
const replacingApiKey = ref(false);
const localProblem = ref<AppProblem>();
const createForm = reactive({
  name: "",
  definitionKey: "" as ProviderDefinitionKey | "",
});
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const definitionsRoot = ref<HTMLElement>();
const definitionsSentinel = ref<HTMLElement>();
const definitionsPageSize = useAdaptiveCursorPageSize({
  container: definitionsRoot,
  itemSelector: ".provider-readiness__row",
  itemCount: () => definitions.value.length,
  estimatedItemHeight: 64,
  estimatedColumns: 1,
});
const accountsRoot = ref<HTMLElement>();
const accountsSentinel = ref<HTMLElement>();
const accountsPageSize = useAdaptiveCursorPageSize({
  container: accountsRoot,
  itemSelector: ".provider-account-row",
  itemCount: () => accounts.value.length,
  estimatedItemHeight: 72,
  estimatedColumns: 1,
});
useCursorInfiniteScroll({
  root: definitionsRoot,
  sentinel: definitionsSentinel,
  enabled: () =>
    Boolean(definitionsNextPageToken.value) && !definitionsLoadingMore.value,
  loadMore: () => store.loadMoreDefinitions(definitionsPageSize.value),
});
useCursorInfiniteScroll({
  root: accountsRoot,
  sentinel: accountsSentinel,
  enabled: () =>
    Boolean(accountsNextPageToken.value) &&
    !loading.value &&
    !loadingMore.value,
  loadMore: () => store.loadMore(accountsPageSize.value),
});

const canCreate = computed(() =>
  pageAllowsAccountCreation(pageNextActions.value),
);
const authorizedApiKeyRefs = computed(() =>
  accounts.value
    .filter(
      (account) =>
        account.state === "AUTHORIZED" &&
        account.enabled &&
        account.authorization?.method === "API_KEY",
    )
    .map((account) => account.ref)
    .sort()
    .join(","),
);
const speechConfigurationMissing = ref(false);
watch(
  authorizedApiKeyRefs,
  (refs, _previous, cleanup) => {
    speechConfigurationMissing.value = false;
    if (!refs) return;
    const controller = new AbortController();
    cleanup(() => controller.abort());
    void readSpeechAvailability(controller.signal)
      .then((availability) => {
        if (!controller.signal.aborted)
          speechConfigurationMissing.value =
            availability.reason === "STT_NOT_CONFIGURED";
      })
      .catch(() => {
        if (!controller.signal.aborted)
          speechConfigurationMissing.value = false;
      });
  },
  { immediate: true },
);
const availableDefinitions = computed(() =>
  definitions.value.filter((item) => item.available),
);
const selectedDefinition = computed(() => {
  const definition = definitions.value.find(
    (item) => item.key === createForm.definitionKey,
  );
  return definition
    ? {
        ref: definition.key,
        title: definition.name,
        description: definition.description,
      }
    : undefined;
});
async function searchDefinitions(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const page = await loadProviderDefinitions(query, cursor, signal, pageSize);
  if (!Array.isArray(page.items) || typeof page.nextPageToken !== "string")
    throw new Error("Invalid provider definition catalog");
  return {
    items: page.items.map((definition) => ({
      ref: definition.key,
      title: definition.name,
      description: definition.description,
      disabled: !definition.available,
      disabledReason: definition.available
        ? undefined
        : definition.readinessBlockers
            .map((code) => t(blockerLabel(code)))
            .join("; ") || t("common.unavailable"),
    })),
    nextPageToken: page.nextPageToken || undefined,
  };
}
function chooseDefinition(value: unknown): void {
  createForm.definitionKey = value === "openai-codex" ? value : "";
}
const authorizationDefinition = computed(() =>
  definitions.value.find(
    (item) => item.key === authorizationAccount.value?.definitionKey,
  ),
);
const authorizationMethods = computed(
  () => authorizationDefinition.value?.authorizationMethods ?? [],
);

function providerName(definitionKey: string): string {
  return (
    definitions.value.find((item) => item.key === definitionKey)?.name ??
    "Provider"
  );
}

function blockerLabel(code: string): string {
  return `providers.blockers.${readableProviderBlocker(code)}`;
}

function scheduleSearch(): void {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(
    () =>
      void store.load(
        search.value,
        accountsPageSize.value,
        definitionsPageSize.value,
      ),
    500,
  );
}

function reload(): Promise<void> {
  return store.load(
    search.value,
    accountsPageSize.value,
    definitionsPageSize.value,
  );
}

function openCreate(): void {
  if (!canCreate.value) return;
  createForm.name = "";
  createForm.definitionKey = availableDefinitions.value[0]?.key ?? "";
  createOpen.value = true;
  localProblem.value = undefined;
}

async function createAccount(): Promise<void> {
  if (!createForm.definitionKey || !createForm.name.trim() || !canCreate.value)
    return;
  localProblem.value = undefined;
  try {
    const account = await store.create({
      definitionKey: createForm.definitionKey,
      name: createForm.name.trim(),
    });
    createOpen.value = false;
    openAuthorization(account);
  } catch (error) {
    localProblem.value = asProblem(error);
  }
}

function openAuthorization(account: ProviderAccount): void {
  authorizationAccount.value = account;
  authorizationMethod.value =
    account.authorization?.method ??
    (authorizationMethods.value.includes("DEVICE_CODE")
      ? "DEVICE_CODE"
      : "API_KEY");
  apiKey.value = "";
  replacingApiKey.value = false;
  localProblem.value = undefined;
}

function closeAuthorization(): void {
  apiKey.value = "";
  authorizationAccount.value = undefined;
  localProblem.value = undefined;
}

function syncAuthorizationAccount(account: ProviderAccount): void {
  if (authorizationAccount.value?.ref === account.ref)
    authorizationAccount.value = account;
}

async function startDevice(): Promise<void> {
  const account = authorizationAccount.value;
  if (!account || authorizationRecoveryPending.value) return;
  localProblem.value = undefined;
  try {
    syncAuthorizationAccount(await store.startDevice(account));
  } catch (error) {
    if (authorizationAccount.value?.ref === account.ref)
      localProblem.value = asProblem(error);
  }
}

async function reauthorize(): Promise<void> {
  const account = authorizationAccount.value;
  if (!account || authorizationRecoveryPending.value) return;
  localProblem.value = undefined;
  try {
    syncAuthorizationAccount(await store.startDevice(account, true));
  } catch (error) {
    if (authorizationAccount.value?.ref === account.ref)
      localProblem.value = asProblem(error);
  }
}

async function refreshAuthorization(): Promise<void> {
  const account = authorizationAccount.value;
  if (!account || authorizationRecoveryPending.value) return;
  localProblem.value = undefined;
  try {
    syncAuthorizationAccount(await store.refreshAuthorization(account));
  } catch (error) {
    if (authorizationAccount.value?.ref === account.ref)
      localProblem.value = asProblem(error);
  }
}

async function submitApiKey(): Promise<void> {
  const account = authorizationAccount.value;
  if (
    !account ||
    !apiKey.value ||
    !accountAllows(account, "CONFIGURE_CREDENTIAL")
  )
    return;
  const credential = apiKey.value;
  apiKey.value = "";
  localProblem.value = undefined;
  try {
    syncAuthorizationAccount(await store.authorizeApiKey(account, credential));
    replacingApiKey.value = false;
  } catch (error) {
    if (authorizationAccount.value?.ref === account.ref)
      localProblem.value = asProblem(error);
  }
}

async function changeEnabled(account: ProviderAccount): Promise<void> {
  try {
    await store.setEnabled(account, !account.enabled);
  } catch (error) {
    localProblem.value = asProblem(error);
  }
}

async function revoke(account: ProviderAccount): Promise<boolean> {
  try {
    const updated = await store.revoke(account);
    if (authorizationAccount.value?.ref === updated.ref)
      authorizationAccount.value = updated;
    return true;
  } catch (error) {
    localProblem.value = asProblem(error);
    return false;
  }
}

function requestRevoke(account: ProviderAccount): void {
  if (!accountAllows(account, "REVOKE")) return;
  revokeAccount.value = account;
  localProblem.value = undefined;
}

function requestDelete(account: ProviderAccount): void {
  if (!accountAllows(account, "DELETE")) return;
  closeAuthorization();
  impactAccount.value = account;
  localProblem.value = undefined;
}

async function confirmRevoke(): Promise<void> {
  const account = revokeAccount.value;
  if (!account) return;
  if (!(await revoke(account))) return;
  revokeAccount.value = undefined;
}

function receiveLifecycleAccount(account: ProviderAccount): void {
  store.accounts = upsertProviderAccount(store.accounts, account);
  if (authorizationAccount.value?.ref === account.ref) {
    if (account.state === "DELETED") closeAuthorization();
    else authorizationAccount.value = account;
  }
}
function recovered(result: ProviderLifecycleResult): void {
  localProblem.value = undefined;
  store.problem = undefined;
  receiveLifecycleAccount(result.account);
  if (
    authorizationAccount.value?.ref === result.account.ref &&
    hasPendingDeviceAuthorization(result.account)
  )
    store.schedulePoll(result.account.ref);
}

let verificationController = new AbortController();
let verificationTimer: ReturnType<typeof setTimeout> | undefined;
let verificationGeneration = 0;
let verificationReads = 0;
function stopVerificationObservation(): void {
  verificationGeneration++;
  verificationController.abort();
  verificationController = new AbortController();
  clearTimeout(verificationTimer);
}
function scheduleVerificationObservation(): void {
  if (
    authorizationAccount.value?.verification?.state !== "PENDING" ||
    verificationReads >= 150
  )
    return;
  verificationTimer = setTimeout(() => void observeVerification(), 4000);
}
async function observeVerification(): Promise<void> {
  const current = authorizationAccount.value;
  if (!current || current.verification?.state !== "PENDING") return;
  if (
    busyRefs.value.includes(current.ref) ||
    authorizationRecoveryPending.value
  ) {
    scheduleVerificationObservation();
    return;
  }
  const generation = verificationGeneration;
  verificationReads++;
  try {
    const next = await loadProviderAccount(
      current.ref,
      verificationController.signal,
    );
    if (generation !== verificationGeneration) return;
    if (next.ref !== current.ref)
      throw new Error("Provider verification observation scope changed");
    if (
      next.version >= (authorizationAccount.value?.version ?? current.version)
    )
      receiveLifecycleAccount(next);
    scheduleVerificationObservation();
  } catch (error) {
    if (generation !== verificationGeneration) return;
    store.accounts = store.accounts.filter((item) => item.ref !== current.ref);
    closeAuthorization();
    localProblem.value = asProblem(error);
  }
}
watch(
  () => [
    authorizationAccount.value?.ref,
    authorizationAccount.value?.verification?.ref,
    authorizationAccount.value?.verification?.state,
  ],
  () => {
    stopVerificationObservation();
    verificationReads = 0;
    scheduleVerificationObservation();
  },
);

async function copyUserCode(): Promise<void> {
  const code = authorizationAccount.value?.authorization?.userCode;
  if (!code) return;
  try {
    await navigator.clipboard.writeText(code);
  } catch (error) {
    localProblem.value = asProblem(error);
  }
}

onMounted(
  () =>
    void store.load(
      undefined,
      accountsPageSize.value,
      definitionsPageSize.value,
    ),
);
watch(accounts, (items) => {
  const currentRef = authorizationAccount.value?.ref;
  if (!currentRef) return;
  const updated = items.find((item) => item.ref === currentRef);
  if (updated) authorizationAccount.value = updated;
  else closeAuthorization();
});
watch(authorizationMethod, () => {
  apiKey.value = "";
  replacingApiKey.value = false;
});
onBeforeUnmount(() => {
  stopVerificationObservation();
  if (searchTimer) clearTimeout(searchTimer);
  store.stopAllPolling();
  apiKey.value = "";
});
</script>

<template>
  <section class="providers-workspace">
    <header class="providers-toolbar">
      <label class="providers-toolbar__search" :for="searchId">
        <Search :size="17" aria-hidden="true" />
        <span class="sr-only">{{ $t("providers.search") }}</span>
        <input
          :id="searchId"
          v-model="search"
          name="provider-account-search"
          type="search"
          :placeholder="$t('providers.searchPlaceholder')"
          @input="scheduleSearch"
        />
      </label>
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('common.retry')"
        @click="reload"
      >
        <RefreshCw :size="17" aria-hidden="true" />
      </button>
      <button
        class="button button--primary"
        type="button"
        :disabled="!canCreate"
        @click="openCreate"
      >
        <Plus :size="17" aria-hidden="true" />{{ $t("providers.create") }}
      </button>
      <button
        v-if="accounts.length > 6 || accountsNextPageToken"
        class="icon-button"
        :aria-label="$t('catalog.expand')"
        :title="$t('catalog.expand')"
        @click="expanded = true"
      >
        <Maximize2 :size="17" />
      </button>
    </header>

    <aside
      v-if="speechConfigurationMissing"
      class="provider-speech-setup"
      role="status"
    >
      <p>{{ $t("providers.speechSetupRequired") }}</p>
      <RouterLink class="button" to="/configurations/SYSTEM_STT">
        {{ $t("providers.configureSpeech") }}
      </RouterLink>
    </aside>

    <section
      ref="definitionsRoot"
      class="provider-readiness"
      :aria-label="$t('providers.definitions')"
    >
      <table v-if="definitions.length" class="provider-readiness__table">
        <thead>
          <tr>
            <th scope="col">{{ $t("providers.definition") }}</th>
            <th scope="col">{{ $t("common.status") }}</th>
            <th scope="col">{{ $t("common.details") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="definition in definitions"
            :key="definition.key"
            class="provider-readiness__row"
          >
            <td>
              <strong>{{ definition.name }}</strong>
              <small>{{ definition.description }}</small>
            </td>
            <td>
              <StatusBadge
                :state="definition.ready ? 'READY' : 'UNAVAILABLE'"
              />
            </td>
            <td>
              <span v-if="!definition.readinessBlockers.length">{{
                $t("providers.noReadinessBlockers")
              }}</span>
              <ul v-else>
                <li
                  v-for="blocker in definition.readinessBlockers"
                  :key="blocker"
                >
                  {{ $t(blockerLabel(blocker)) }}
                </li>
              </ul>
            </td>
          </tr>
        </tbody>
      </table>
      <div
        v-if="definitionsNextPageToken"
        ref="definitionsSentinel"
        class="provider-readiness__more"
      >
        <LoaderCircle
          v-if="definitionsLoadingMore"
          class="spin"
          :size="16"
          aria-hidden="true"
        />
      </div>
    </section>

    <AsyncState
      :loading="loading && !accounts.length"
      :problem="accounts.length ? undefined : problem"
      @retry="reload"
    >
      <component
        :is="expanded ? ModalDialog : 'div'"
        :title="expanded ? $t('providers.title') : undefined"
        size="full"
        @close="expanded = false"
      >
        <label
          v-if="expanded"
          class="providers-toolbar__search"
          :for="expandedSearchId"
        >
          <Search :size="17" /><span class="sr-only">{{
            $t("providers.search")
          }}</span>
          <input
            :id="expandedSearchId"
            v-model="search"
            name="provider-account-expanded-search"
            type="search"
            :placeholder="$t('providers.searchPlaceholder')"
            @input="scheduleSearch"
          />
        </label>
        <ProblemNotice
          v-if="
            problem &&
            accounts.length &&
            !authorizationAccount &&
            !revokeAccount
          "
          :problem="problem"
        />
        <div
          v-if="accounts.length"
          ref="accountsRoot"
          class="provider-account-list"
          :class="{ 'provider-account-list--expanded': expanded }"
        >
          <table class="provider-account-list__table">
            <thead>
              <tr>
                <th scope="col">{{ $t("providers.tableAccount") }}</th>
                <th scope="col">{{ $t("common.status") }}</th>
                <th scope="col">{{ $t("providers.tableAvailability") }}</th>
                <th scope="col">{{ $t("providers.tableExternalAccount") }}</th>
                <th scope="col">{{ $t("providers.tableUsage") }}</th>
                <th scope="col">{{ $t("common.actions") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="account in accounts"
                :key="account.ref"
                class="provider-account-row"
              >
                <td>
                  <div class="provider-account-row__identity">
                    <span class="provider-account-row__icon"
                      ><KeyRound :size="18" aria-hidden="true"
                    /></span>
                    <div>
                      <strong :title="account.name">{{ account.name }}</strong>
                      <small>{{ providerName(account.definitionKey) }}</small>
                    </div>
                  </div>
                </td>
                <td><StatusBadge :state="account.state" /></td>
                <td>
                  <span v-if="account.authorization?.method">{{
                    $t(`providers.methods.${account.authorization.method}`)
                  }}</span>
                  <span v-else>{{ $t("common.noData") }}</span>
                  <small
                    v-if="
                      account.safeStatusReason &&
                      account.safeStatusReason !== 'AUTHORIZED'
                    "
                    >{{
                      $t(`providers.reasons.${account.safeStatusReason}`)
                    }}</small
                  >
                </td>
                <td>
                  {{
                    account.externalAccountMasked ||
                    $t("providers.externalAccountPending")
                  }}
                </td>
                <td><ProviderUsageDetails :usage="account.usage" /></td>
                <td>
                  <nav
                    class="provider-account-row__actions"
                    :aria-label="account.name"
                  >
                    <button
                      type="button"
                      class="icon-button"
                      :title="$t('providerLifecycle.title')"
                      :aria-label="$t('providerLifecycle.title')"
                      :disabled="busyRefs.includes(account.ref)"
                      @click="impactAccount = account"
                    >
                      <Info :size="18" aria-hidden="true" />
                    </button>
                    <button
                      v-if="
                        account.authorization?.method === 'DEVICE_CODE' &&
                        accountAllows(account, 'REFRESH_AUTHORIZATION')
                      "
                      type="button"
                      class="icon-button"
                      :title="$t('providers.checkAuthorization')"
                      :aria-label="$t('providers.checkAuthorization')"
                      :disabled="busyRefs.includes(account.ref)"
                      @click="
                        openAuthorization(account);
                        refreshAuthorization();
                      "
                    >
                      <RefreshCw :size="18" aria-hidden="true" />
                    </button>
                    <button
                      v-if="accountAllows(account, 'CONFIGURE_CREDENTIAL')"
                      type="button"
                      class="icon-button"
                      :title="
                        $t(
                          account.state === 'AUTHORIZED'
                            ? 'providers.reauthorize'
                            : 'providers.authorize',
                        )
                      "
                      :aria-label="
                        $t(
                          account.state === 'AUTHORIZED'
                            ? 'providers.reauthorize'
                            : 'providers.authorize',
                        )
                      "
                      :disabled="busyRefs.includes(account.ref)"
                      @click="openAuthorization(account)"
                    >
                      <KeyRound :size="18" aria-hidden="true" />
                    </button>
                    <button
                      v-if="
                        accountAllows(
                          account,
                          account.enabled ? 'DISABLE' : 'ENABLE',
                        )
                      "
                      type="button"
                      class="icon-button"
                      :title="
                        $t(account.enabled ? 'common.disable' : 'common.enable')
                      "
                      :aria-label="
                        $t(account.enabled ? 'common.disable' : 'common.enable')
                      "
                      :disabled="busyRefs.includes(account.ref)"
                      @click="changeEnabled(account)"
                    >
                      <PowerOff
                        v-if="account.enabled"
                        :size="18"
                        aria-hidden="true"
                      /><Power v-else :size="18" aria-hidden="true" />
                    </button>
                    <button
                      v-if="accountAllows(account, 'REVOKE')"
                      type="button"
                      class="icon-button icon-button--danger"
                      :title="$t('providers.revoke')"
                      :aria-label="$t('providers.revoke')"
                      :disabled="busyRefs.includes(account.ref)"
                      @click="requestRevoke(account)"
                    >
                      <ShieldOff :size="18" aria-hidden="true" />
                    </button>
                    <button
                      v-if="accountAllows(account, 'DELETE')"
                      type="button"
                      class="icon-button icon-button--danger"
                      :title="$t('common.delete')"
                      :aria-label="$t('common.delete')"
                      :disabled="busyRefs.includes(account.ref)"
                      @click="requestDelete(account)"
                    >
                      <Trash2 :size="18" aria-hidden="true" />
                    </button>
                  </nav>
                </td>
              </tr>
            </tbody>
          </table>
          <div
            v-if="accountsNextPageToken"
            ref="accountsSentinel"
            class="providers-load-more"
          >
            <LoaderCircle
              v-if="loadingMore"
              class="spin"
              :size="16"
              aria-hidden="true"
            />
          </div>
        </div>
        <section v-else class="empty-state">
          <KeyRound :size="28" aria-hidden="true" />
          <h2>
            {{
              $t(
                search.trim()
                  ? "providers.searchEmptyTitle"
                  : "providers.emptyTitle",
              )
            }}
          </h2>
          <p>
            {{
              $t(
                search.trim()
                  ? "providers.searchEmptyText"
                  : "providers.emptyText",
              )
            }}
          </p>
        </section>
      </component>
    </AsyncState>

    <ProblemNotice
      v-if="
        localProblem && !createOpen && !authorizationAccount && !revokeAccount
      "
      :problem="localProblem"
    />

    <ModalDialog
      v-if="createOpen"
      :title="$t('providers.create')"
      :busy="busyRefs.includes('create')"
      size="md"
      @close="createOpen = false"
    >
      <ProblemNotice v-if="localProblem" :problem="localProblem" />
      <div class="provider-form">
        <label class="field">
          <span>{{ $t("common.name") }}</span>
          <input
            :id="createNameId"
            v-model="createForm.name"
            name="provider-account-name"
            maxlength="160"
            autocomplete="off"
          />
        </label>
        <div class="field">
          <span>{{ $t("providers.definition") }}</span>
          <AsyncEntityPicker
            :model-value="createForm.definitionKey || null"
            :selected="selectedDefinition"
            :load-page="searchDefinitions"
            :trigger-label="$t('providers.definition')"
            :disabled="busyRefs.includes('create')"
            @update:model-value="chooseDefinition"
          />
        </div>
      </div>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busyRefs.includes('create')"
          @click="createOpen = false"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--primary"
          type="button"
          :disabled="
            !createForm.name.trim() ||
            !createForm.definitionKey ||
            busyRefs.includes('create')
          "
          @click="createAccount"
        >
          {{ $t("common.create") }}
        </button>
      </template>
    </ModalDialog>

    <ModalDialog
      v-if="authorizationAccount"
      :title="
        $t('providers.authorizationTitle', { name: authorizationAccount.name })
      "
      :busy="busyRefs.includes(authorizationAccount.ref)"
      size="lg"
      @close="closeAuthorization"
    >
      <ProblemNotice
        v-if="localProblem || problem"
        :problem="localProblem ?? problem"
      />
      <ProviderLifecycleRecovery
        :account="authorizationAccount"
        :problem="localProblem ?? problem"
        @pending="authorizationRecoveryPending = $event"
        @recovered="recovered"
      />
      <section
        v-if="authorizationAccount.verification"
        class="provider-verification"
        role="status"
      >
        <strong>{{ $t("providerLifecycle.verification") }}</strong>
        <p>
          {{
            $t(
              `providerLifecycle.reasons.${authorizationAccount.verification.safeReason}`,
            )
          }}
        </p>
        <p>{{ $t("providerLifecycle.verificationScope") }}</p>
        <span
          >{{ $t("providerLifecycle.requested") }}:
          <time :datetime="authorizationAccount.verification.requestedAt">{{
            authorizationAccount.verification.requestedAt
          }}</time></span
        >
        <span v-if="authorizationAccount.verification.completedAt"
          >{{ $t("providerLifecycle.completed") }}:
          <time :datetime="authorizationAccount.verification.completedAt">{{
            authorizationAccount.verification.completedAt
          }}</time></span
        >
      </section>
      <fieldset
        class="authorization-boundary"
        :disabled="
          authorizationRecoveryPending ||
          busyRefs.includes(authorizationAccount.ref)
        "
      >
        <div class="authorization-dialog">
          <div
            class="authorization-methods"
            role="tablist"
            :aria-label="$t('providers.authorizationMethod')"
          >
            <button
              v-for="method in authorizationMethods"
              :key="method"
              class="button"
              :class="{ 'button--primary': authorizationMethod === method }"
              type="button"
              role="tab"
              :aria-selected="authorizationMethod === method"
              :disabled="busyRefs.includes(authorizationAccount.ref)"
              @click="authorizationMethod = method"
            >
              <Smartphone
                v-if="method === 'DEVICE_CODE'"
                :size="16"
                aria-hidden="true"
              />
              <KeyRound v-else :size="16" aria-hidden="true" />
              {{ $t(`providers.methods.${method}`) }}
            </button>
          </div>

          <section
            v-if="authorizationMethod === 'DEVICE_CODE'"
            class="authorization-panel"
          >
            <template v-if="isPendingDeviceAuthorization(authorizationAccount)">
              <a
                v-if="
                  safeVerificationUri(
                    authorizationAccount.authorization?.verificationUri,
                  )
                "
                class="button button--primary"
                :href="
                  safeVerificationUri(
                    authorizationAccount.authorization?.verificationUri,
                  ) ?? undefined
                "
                target="_blank"
                rel="noopener noreferrer"
                >{{ $t("providers.openVerification") }}</a
              >
              <div class="device-code">
                <span>{{ $t("providers.userCode") }}</span>
                <code>{{ authorizationAccount.authorization?.userCode }}</code>
                <button
                  class="icon-button"
                  type="button"
                  :aria-label="$t('providers.copyCode')"
                  @click="copyUserCode"
                >
                  <Copy :size="17" aria-hidden="true" />
                </button>
              </div>
              <p
                v-if="authorizationAccount.authorization?.expiresAt"
                class="muted"
              >
                {{
                  $t("providers.expiresAt", {
                    value: new Date(
                      authorizationAccount.authorization.expiresAt,
                    ).toLocaleString(),
                  })
                }}
              </p>
              <p
                v-if="pollingRefs.includes(authorizationAccount.ref)"
                role="status"
                class="polling-state"
              >
                <LoaderCircle class="spin" :size="17" aria-hidden="true" />{{
                  $t("providers.waitingAuthorization")
                }}
              </p>
              <button
                v-if="
                  accountAllows(authorizationAccount, 'REFRESH_AUTHORIZATION')
                "
                class="button"
                type="button"
                :disabled="busyRefs.includes(authorizationAccount.ref)"
                @click="refreshAuthorization"
              >
                {{ $t("providers.checkAuthorization") }}
              </button>
            </template>
            <template
              v-else-if="
                authorizationAccount.authorization?.state === 'AUTHORIZED'
              "
            >
              <Check :size="24" aria-hidden="true" />
              <strong>{{ $t("providers.authorized") }}</strong>
              <button
                v-if="
                  accountAllows(authorizationAccount, 'REFRESH_AUTHORIZATION')
                "
                class="button"
                :disabled="busyRefs.includes(authorizationAccount.ref)"
                @click="refreshAuthorization"
              >
                <RefreshCw :size="16" />{{ $t("providers.checkAuthorization") }}
              </button>
              <button
                v-if="
                  accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
                "
                class="button"
                :disabled="busyRefs.includes(authorizationAccount.ref)"
                @click="reauthorize"
              >
                {{ $t("providers.reauthorize") }}
              </button>
            </template>
            <template v-else>
              <button
                v-if="
                  accountAllows(authorizationAccount, 'REFRESH_AUTHORIZATION')
                "
                class="button"
                :disabled="busyRefs.includes(authorizationAccount.ref)"
                @click="refreshAuthorization"
              >
                <RefreshCw :size="16" />{{ $t("providers.checkAuthorization") }}
              </button>
              <button
                class="button button--primary"
                type="button"
                :disabled="
                  busyRefs.includes(authorizationAccount.ref) ||
                  !accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
                "
                @click="startDevice"
              >
                {{ $t("providers.startDevice") }}
              </button>
            </template>
          </section>

          <section v-else class="authorization-panel">
            <template
              v-if="
                authorizationAccount.authorization?.state === 'AUTHORIZED' &&
                !replacingApiKey
              "
            >
              <Check :size="24" aria-hidden="true" />
              <strong>{{ $t("providers.authorized") }}</strong>
              <button
                v-if="
                  accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
                "
                class="button"
                :disabled="busyRefs.includes(authorizationAccount.ref)"
                @click="replacingApiKey = true"
              >
                <KeyRound :size="16" />{{ $t("providers.reauthorize") }}
              </button>
            </template>
            <form v-else class="provider-form" @submit.prevent="submitApiKey">
              <label class="field">
                <span>{{ $t("providers.apiKey") }}</span>
                <input
                  :id="apiKeyId"
                  v-model="apiKey"
                  name="provider-account-api-key"
                  type="password"
                  maxlength="16384"
                  autocomplete="off"
                  spellcheck="false"
                  :placeholder="$t('providers.apiKeyPlaceholder')"
                />
                <small>{{ $t("providers.apiKeySafety") }}</small>
              </label>
              <button
                class="button button--primary"
                type="submit"
                :disabled="
                  !apiKey ||
                  busyRefs.includes(authorizationAccount.ref) ||
                  !accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
                "
              >
                {{ $t("providers.authorizeApiKey") }}
              </button>
            </form>
          </section>
          <div
            v-if="authorizationAccount.authorization?.state === 'FAILED'"
            class="safe-warning"
            role="alert"
          >
            <CircleAlert :size="18" aria-hidden="true" />{{
              $t("providers.authorizationFailed")
            }}
          </div>
        </div>
      </fieldset>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busyRefs.includes(authorizationAccount.ref)"
          @click="closeAuthorization"
        >
          {{ $t("common.close") }}
        </button>
        <button
          v-if="accountAllows(authorizationAccount, 'DELETE')"
          class="button button--danger"
          :disabled="busyRefs.includes(authorizationAccount.ref)"
          @click="requestDelete(authorizationAccount)"
        >
          <Trash2 :size="16" />{{ $t("common.delete") }}
        </button>
      </template>
    </ModalDialog>

    <ModalDialog
      v-if="impactAccount"
      :title="$t('providerLifecycle.title')"
      size="lg"
      @close="impactAccount = undefined"
    >
      <ProviderAccountLifecyclePanel
        :account="impactAccount"
        @updated="receiveLifecycleAccount"
        @unavailable="
          store.accounts = store.accounts.filter((item) => item.ref !== $event)
        "
      />
    </ModalDialog>

    <ModalDialog
      v-if="revokeAccount"
      :title="$t('providers.revokeTitle')"
      :busy="busyRefs.includes(revokeAccount.ref)"
      size="sm"
      @close="revokeAccount = undefined"
    >
      <ProblemNotice v-if="localProblem" :problem="localProblem" />
      <p>
        {{ $t("providers.revokeConfirmation", { name: revokeAccount.name }) }}
      </p>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busyRefs.includes(revokeAccount.ref)"
          @click="revokeAccount = undefined"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--danger"
          type="button"
          :disabled="busyRefs.includes(revokeAccount.ref)"
          @click="confirmRevoke"
        >
          <ShieldOff :size="16" aria-hidden="true" />{{
            $t("providers.revoke")
          }}
        </button>
      </template>
    </ModalDialog>
  </section>
</template>

<style scoped>
.authorization-boundary {
  border: 0;
  padding: 0;
  min-width: 0;
}
.provider-verification {
  display: grid;
  gap: 8px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.providers-workspace {
  display: grid;
  gap: 16px;
}
.providers-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.providers-toolbar__search {
  display: flex;
  min-width: 240px;
  flex: 1;
  align-items: center;
  gap: 8px;
}
.providers-toolbar__search input {
  min-width: 0;
  flex: 1;
}
.provider-readiness {
  min-width: 0;
  overflow-x: auto;
}
.provider-readiness__table,
.provider-account-list__table {
  width: 100%;
  border-collapse: collapse;
  table-layout: fixed;
  border: 1px solid var(--border);
  background: var(--surface);
}
.provider-readiness__table {
  min-width: 720px;
}
.provider-readiness__table th:first-child {
  width: 42%;
}
.provider-readiness__table th:nth-child(2) {
  width: 18%;
}
.provider-readiness__table th:last-child {
  width: 40%;
}
.provider-readiness__table th,
.provider-readiness__table td,
.provider-account-list__table th,
.provider-account-list__table td {
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}
.provider-readiness__table th,
.provider-account-list__table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.provider-readiness__table tbody tr + tr,
.provider-account-list__table tbody tr + tr {
  border-top: 1px solid var(--border);
}
.provider-readiness__table td:first-child small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
}
.provider-readiness__more {
  min-height: 24px;
}
.provider-readiness ul {
  margin: 0;
  padding-left: 16px;
}
.provider-account-list {
  min-width: 0;
  max-height: 1000px;
  overflow: auto;
}
.provider-account-list__table {
  min-width: 1120px;
}
.provider-account-list__table th:first-child {
  width: 20%;
}
.provider-account-list__table th:nth-child(2) {
  width: 13%;
}
.provider-account-list__table th:nth-child(3) {
  width: 14%;
}
.provider-account-list__table th:nth-child(4) {
  width: 15%;
}
.provider-account-list__table th:nth-child(5) {
  width: 25%;
}
.provider-account-list__table th:last-child {
  width: 13%;
}
.provider-speech-setup {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.provider-speech-setup p {
  margin: 0;
}
.provider-account-list--expanded {
  max-height: calc(100dvh - 230px);
}
.provider-account-row__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.provider-account-row__identity > div {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.provider-account-row__identity strong,
.provider-account-row__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-account-row__identity small {
  color: var(--muted);
  font-size: 0.8rem;
}
.provider-account-row__icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  color: var(--accent-strong);
}
.provider-account-row td {
  overflow-wrap: anywhere;
  font-size: 0.82rem;
}
.provider-account-row td small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
}
.provider-account-row__actions {
  display: flex;
  justify-content: flex-end;
  gap: 2px;
  white-space: nowrap;
}
.provider-account-row__actions .icon-button--danger {
  color: var(--danger);
}
.providers-load-more {
  display: flex;
  justify-content: center;
}
.provider-form,
.authorization-dialog,
.authorization-panel {
  display: grid;
  gap: 14px;
}
.authorization-methods {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.authorization-panel {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
}
.authorization-panel > p {
  margin: 0;
}
.device-code {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.device-code span {
  grid-column: 1 / -1;
  color: var(--muted);
  font-size: 0.78rem;
}
.device-code code {
  overflow-wrap: anywhere;
  font-size: 1.2rem;
  font-weight: 700;
}
.polling-state,
.safe-warning {
  display: flex;
  align-items: center;
  gap: 8px;
}
.safe-warning {
  padding: 10px;
  border: 1px solid var(--warning);
  border-radius: 6px;
  color: var(--warning);
}
.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 560px) {
  .provider-speech-setup {
    align-items: stretch;
    flex-direction: column;
  }
  .providers-toolbar,
  .authorization-methods {
    align-items: stretch;
    flex-direction: column;
  }
  .providers-toolbar__search {
    min-width: 0;
  }
}
</style>
