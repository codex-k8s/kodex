<script setup lang="ts">
import {
  Check,
  CircleAlert,
  Copy,
  KeyRound,
  LoaderCircle,
  Plus,
  RefreshCw,
  Search,
  ShieldOff,
  Smartphone,
} from "@lucide/vue";
import { storeToRefs } from "pinia";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";

import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

import {
  accountAllows,
  isPendingDeviceAuthorization,
  pageAllowsAccountCreation,
  readableProviderBlocker,
  safeVerificationUri,
  type ProviderAccount,
  type ProviderAuthorizationMethod,
  type ProviderDefinitionKey,
} from "./model";
import { useProvidersStore } from "./store";

const store = useProvidersStore();
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
const createOpen = ref(false);
const authorizationAccount = ref<ProviderAccount>();
const revokeAccount = ref<ProviderAccount>();
const authorizationMethod = ref<ProviderAuthorizationMethod>("DEVICE_CODE");
const apiKey = ref("");
const localProblem = ref<AppProblem>();
const createForm = reactive({
  name: "",
  definitionKey: "" as ProviderDefinitionKey | "",
});
let searchTimer: ReturnType<typeof setTimeout> | undefined;

const canCreate = computed(() =>
  pageAllowsAccountCreation(pageNextActions.value),
);
const availableDefinitions = computed(() =>
  definitions.value.filter((item) => item.available),
);
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
  searchTimer = setTimeout(() => void store.load(search.value), 280);
}

function openCreate(): void {
  if (!canCreate.value) return;
  createForm.name = "";
  createForm.definitionKey = availableDefinitions.value[0]?.key ?? "";
  createOpen.value = true;
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
  authorizationMethod.value = authorizationMethods.value.includes("DEVICE_CODE")
    ? "DEVICE_CODE"
    : "API_KEY";
  apiKey.value = "";
  localProblem.value = undefined;
}

function closeAuthorization(): void {
  if (authorizationAccount.value)
    store.stopPolling(authorizationAccount.value.ref);
  apiKey.value = "";
  authorizationAccount.value = undefined;
  localProblem.value = undefined;
}

function syncAuthorizationAccount(account: ProviderAccount): void {
  authorizationAccount.value = account;
}

async function startDevice(): Promise<void> {
  const account = authorizationAccount.value;
  if (!account) return;
  try {
    syncAuthorizationAccount(await store.startDevice(account));
  } catch (error) {
    localProblem.value = asProblem(error);
  }
}

async function refreshAuthorization(): Promise<void> {
  const account = authorizationAccount.value;
  if (!account) return;
  try {
    syncAuthorizationAccount(await store.refreshAuthorization(account));
  } catch (error) {
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
  try {
    syncAuthorizationAccount(await store.authorizeApiKey(account, credential));
  } catch (error) {
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

async function revoke(account: ProviderAccount): Promise<void> {
  try {
    const updated = await store.revoke(account);
    if (authorizationAccount.value?.ref === updated.ref)
      authorizationAccount.value = updated;
  } catch (error) {
    localProblem.value = asProblem(error);
  }
}

function requestRevoke(account: ProviderAccount): void {
  if (!accountAllows(account, "REVOKE")) return;
  revokeAccount.value = account;
}

async function confirmRevoke(): Promise<void> {
  const account = revokeAccount.value;
  if (!account) return;
  await revoke(account);
  revokeAccount.value = undefined;
}

async function copyUserCode(): Promise<void> {
  const code = authorizationAccount.value?.authorization?.userCode;
  if (code) await navigator.clipboard.writeText(code);
}

onMounted(() => void store.load());
watch(accounts, (items) => {
  const currentRef = authorizationAccount.value?.ref;
  if (!currentRef) return;
  const updated = items.find((item) => item.ref === currentRef);
  if (updated) authorizationAccount.value = updated;
});
onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  store.stopAllPolling();
  apiKey.value = "";
});
</script>

<template>
  <section class="providers-workspace">
    <header class="providers-toolbar">
      <label class="providers-toolbar__search">
        <Search :size="17" aria-hidden="true" />
        <span class="sr-only">{{ $t("providers.search") }}</span>
        <input
          v-model="search"
          type="search"
          :placeholder="$t('providers.searchPlaceholder')"
          @input="scheduleSearch"
        />
      </label>
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('common.retry')"
        @click="store.load(search)"
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
    </header>

    <section
      class="provider-readiness"
      :aria-label="$t('providers.definitions')"
    >
      <article v-for="definition in definitions" :key="definition.key">
        <div>
          <strong>{{ definition.name }}</strong>
          <p>{{ definition.description }}</p>
        </div>
        <StatusBadge :state="definition.ready ? 'READY' : 'UNAVAILABLE'" />
        <ul v-if="definition.readinessBlockers.length">
          <li v-for="blocker in definition.readinessBlockers" :key="blocker">
            {{ $t(blockerLabel(blocker)) }}
          </li>
        </ul>
      </article>
      <button
        v-if="definitionsNextPageToken"
        class="button provider-readiness__more"
        type="button"
        :disabled="definitionsLoadingMore"
        @click="store.loadMoreDefinitions"
      >
        <LoaderCircle
          v-if="definitionsLoadingMore"
          class="spin"
          :size="16"
          aria-hidden="true"
        />
        {{ $t("providers.loadMoreProviders") }}
      </button>
    </section>

    <AsyncState
      :loading="loading"
      :problem="problem"
      @retry="store.load(search)"
    >
      <div v-if="accounts.length" class="provider-account-list">
        <article
          v-for="account in accounts"
          :key="account.ref"
          class="provider-account-card"
        >
          <div class="provider-account-card__identity">
            <span class="provider-account-card__icon"
              ><KeyRound :size="20" aria-hidden="true"
            /></span>
            <div>
              <h2>{{ account.name }}</h2>
              <p>{{ providerName(account.definitionKey) }}</p>
            </div>
          </div>
          <div class="provider-account-card__state">
            <StatusBadge
              :state="account.ready ? account.state : 'UNAVAILABLE'"
            />
            <span>{{
              account.externalAccountMasked ||
              $t("providers.externalAccountPending")
            }}</span>
          </div>
          <div class="provider-account-card__actions">
            <button
              v-if="accountAllows(account, 'CONFIGURE_CREDENTIAL')"
              class="button"
              type="button"
              :disabled="busyRefs.includes(account.ref)"
              @click="openAuthorization(account)"
            >
              {{ $t("providers.authorize") }}
            </button>
            <button
              v-if="
                accountAllows(account, account.enabled ? 'DISABLE' : 'ENABLE')
              "
              class="button"
              type="button"
              :disabled="busyRefs.includes(account.ref)"
              @click="changeEnabled(account)"
            >
              {{ $t(account.enabled ? "common.disable" : "common.enable") }}
            </button>
            <button
              v-if="accountAllows(account, 'REVOKE')"
              class="button button--danger"
              type="button"
              :disabled="busyRefs.includes(account.ref)"
              @click="requestRevoke(account)"
            >
              <ShieldOff :size="16" aria-hidden="true" />{{
                $t("providers.revoke")
              }}
            </button>
          </div>
        </article>
        <button
          v-if="accountsNextPageToken"
          class="button providers-load-more"
          type="button"
          :disabled="loadingMore"
          @click="store.loadMore"
        >
          <LoaderCircle
            v-if="loadingMore"
            class="spin"
            :size="16"
            aria-hidden="true"
          />
          {{ $t("providers.loadMore") }}
        </button>
      </div>
      <section v-else class="empty-state">
        <KeyRound :size="28" aria-hidden="true" />
        <h2>{{ $t("providers.emptyTitle") }}</h2>
        <p>{{ $t("providers.emptyText") }}</p>
      </section>
    </AsyncState>

    <ProblemNotice v-if="localProblem" :problem="localProblem" />

    <ModalDialog
      v-if="createOpen"
      :title="$t('providers.create')"
      :busy="busyRefs.includes('create')"
      size="md"
      @close="createOpen = false"
    >
      <div class="provider-form">
        <label class="field">
          <span>{{ $t("common.name") }}</span>
          <input v-model="createForm.name" maxlength="160" autocomplete="off" />
        </label>
        <label class="field">
          <span>{{ $t("providers.definition") }}</span>
          <select v-model="createForm.definitionKey">
            <option
              v-for="definition in availableDefinitions"
              :key="definition.key"
              :value="definition.key"
            >
              {{ definition.name }}
            </option>
          </select>
        </label>
      </div>
      <template #actions>
        <button class="button" type="button" @click="createOpen = false">
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
            <p>{{ $t("providers.deviceInstructions") }}</p>
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
              v-if="accountAllows(authorizationAccount, 'TEST')"
              class="button"
              type="button"
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
          </template>
          <template v-else>
            <p>{{ $t("providers.deviceDescription") }}</p>
            <button
              class="button button--primary"
              type="button"
              :disabled="
                !accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
              "
              @click="startDevice"
            >
              {{ $t("providers.startDevice") }}
            </button>
          </template>
        </section>

        <form v-else class="authorization-panel" @submit.prevent="submitApiKey">
          <p>{{ $t("providers.apiKeyDescription") }}</p>
          <label class="field">
            <span>{{ $t("providers.apiKey") }}</span>
            <input
              v-model="apiKey"
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
              !accountAllows(authorizationAccount, 'CONFIGURE_CREDENTIAL')
            "
          >
            {{ $t("providers.authorizeApiKey") }}
          </button>
        </form>
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
      <template #actions>
        <button class="button" type="button" @click="closeAuthorization">
          {{ $t("common.close") }}
        </button>
      </template>
    </ModalDialog>

    <ModalDialog
      v-if="revokeAccount"
      :title="$t('providers.revokeTitle')"
      :busy="busyRefs.includes(revokeAccount.ref)"
      size="sm"
      @close="revokeAccount = undefined"
    >
      <p>
        {{ $t("providers.revokeConfirmation", { name: revokeAccount.name }) }}
      </p>
      <template #actions>
        <button class="button" type="button" @click="revokeAccount = undefined">
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
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 10px;
}
.provider-readiness article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.provider-readiness p,
.provider-readiness ul {
  margin: 4px 0 0;
  color: var(--muted);
  font-size: 0.82rem;
}
.provider-readiness__more {
  min-height: 52px;
  align-self: stretch;
  justify-self: stretch;
}
.provider-readiness ul {
  grid-column: 1 / -1;
  padding-left: 18px;
}
.provider-account-list {
  display: grid;
  gap: 8px;
}
.provider-account-card {
  display: grid;
  grid-template-columns: minmax(240px, 1fr) minmax(220px, 0.8fr) auto;
  align-items: center;
  gap: 16px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.provider-account-card__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.provider-account-card__icon {
  display: grid;
  width: 38px;
  height: 38px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 6px;
  background: var(--surface);
  color: var(--primary);
}
.provider-account-card h2,
.provider-account-card p {
  margin: 0;
}
.provider-account-card h2 {
  font-size: 0.94rem;
}
.provider-account-card p,
.provider-account-card__state span {
  color: var(--muted);
  font-size: 0.8rem;
}
.provider-account-card__state {
  display: grid;
  justify-items: start;
  gap: 5px;
}
.provider-account-card__actions {
  display: flex;
  justify-content: flex-end;
  gap: 7px;
  flex-wrap: wrap;
}
.providers-load-more {
  justify-self: center;
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
@media (max-width: 900px) {
  .provider-account-card {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .provider-account-card__actions {
    grid-column: 1 / -1;
    justify-content: flex-start;
  }
}
@media (max-width: 560px) {
  .providers-toolbar,
  .authorization-methods {
    align-items: stretch;
    flex-direction: column;
  }
  .providers-toolbar__search {
    min-width: 0;
  }
  .provider-account-card {
    grid-template-columns: 1fr;
  }
  .provider-account-card__actions {
    grid-column: auto;
  }
}
</style>
