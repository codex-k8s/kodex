<script setup lang="ts">
import { Eye, Plus, RotateCw, Search, ShieldCheck, ShieldX } from "@lucide/vue";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useSessionStore } from "@/features/session/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

import type { RuntimeSecret } from "./model";
import { canRuntimeSecretAction, maskedSecretHint } from "./model";
import RuntimeSecretRevealDialog from "./RuntimeSecretRevealDialog.vue";
import RuntimeSecretRevokeDialog from "./RuntimeSecretRevokeDialog.vue";
import RuntimeSecretValueDialog from "./RuntimeSecretValueDialog.vue";
import { useRuntimeSecretsStore } from "./store";

const props = defineProps<{ projectRef: string }>();
const store = useRuntimeSecretsStore();
const session = useSessionStore();
const { locale } = useI18n();
const search = ref("");
const createOpen = ref(false);
const rotateTarget = ref<RuntimeSecret>();
const revealTarget = ref<RuntimeSecret>();
const revokeTarget = ref<RuntimeSecret>();
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function prepareMutation(): void {
  store.clearMutationProblem();
}

function openCreate(): void {
  prepareMutation();
  createOpen.value = true;
}

function openRotate(secret: RuntimeSecret): void {
  if (!canRuntimeSecretAction(secret, "ROTATE")) return;
  prepareMutation();
  rotateTarget.value = secret;
}

function openRevoke(secret: RuntimeSecret): void {
  if (!canRuntimeSecretAction(secret, "REVOKE")) return;
  prepareMutation();
  revokeTarget.value = secret;
}

async function createSecret(
  input: Parameters<typeof store.create>[0],
): Promise<void> {
  try {
    await store.create(input);
    createOpen.value = false;
  } catch {
    // Store передаёт безопасную problem-модель в открытый диалог.
  }
}

async function rotateSecret(
  input: Parameters<typeof store.rotate>[1],
): Promise<void> {
  if (!rotateTarget.value) return;
  try {
    await store.rotate(rotateTarget.value, input);
    rotateTarget.value = undefined;
  } catch {
    // Store передаёт безопасную problem-модель в открытый диалог.
  }
}

async function revokeSecret(): Promise<void> {
  if (!revokeTarget.value) return;
  try {
    await store.revoke(revokeTarget.value);
    revokeTarget.value = undefined;
  } catch {
    // Store передаёт безопасную problem-модель в открытый диалог.
  }
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (
    store.hasMore &&
    element.scrollTop + element.clientHeight >= element.scrollHeight - 96
  )
    void store.loadMore();
}

function restoreReauthenticatedReveal(): void {
  if (revealTarget.value) return;
  const secretRef = session.pendingRuntimeSecretReveal(props.projectRef);
  if (!secretRef) return;
  const secret = store.items.find((item) => item.ref === secretRef);
  if (secret && canRuntimeSecretAction(secret, "REVEAL"))
    revealTarget.value = secret;
}

watch(search, (value) => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void store.load(props.projectRef, value), 300);
});
watch(
  () => props.projectRef,
  (value) => {
    search.value = "";
    void store.load(value);
  },
);
watch(
  () => store.items.map((item) => item.ref).join("\u0000"),
  restoreReauthenticatedReveal,
  { immediate: true },
);
onMounted(() => void store.load(props.projectRef));
onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  store.dispose();
});
</script>

<template>
  <section class="runtime-secrets panel">
    <header class="runtime-secrets__toolbar">
      <label class="runtime-secrets__search">
        <Search :size="17" aria-hidden="true" />
        <span class="sr-only">{{ $t("runtimeSecrets.search") }}</span>
        <input
          v-model="search"
          type="search"
          :placeholder="$t('runtimeSecrets.searchPlaceholder')"
        />
      </label>
      <div class="runtime-secrets__toolbar-meta">
        <span>{{
          $t("runtimeSecrets.shown", { count: store.items.length })
        }}</span>
        <button
          class="button button--primary"
          type="button"
          :disabled="Boolean(store.busyRef)"
          @click="openCreate"
        >
          <Plus :size="16" aria-hidden="true" />
          {{ $t("runtimeSecrets.create") }}
        </button>
      </div>
    </header>

    <AsyncState
      :loading="store.loading"
      :problem="store.items.length ? undefined : store.problem"
      :empty="store.empty"
      :empty-title="$t('runtimeSecrets.emptyTitle')"
      :empty-text="
        search
          ? $t('runtimeSecrets.emptySearchText')
          : $t('runtimeSecrets.emptyText')
      "
      @retry="store.reload"
    >
      <ProblemNotice
        v-if="store.problem && store.items.length"
        :problem="store.problem"
        @retry="store.loadMore"
      />
      <div class="runtime-secrets__scroll" @scroll.passive="onScroll">
        <table class="runtime-secrets__table">
          <thead>
            <tr>
              <th>{{ $t("runtimeSecrets.secret") }}</th>
              <th>{{ $t("runtimeSecrets.maskedHint") }}</th>
              <th>{{ $t("runtimeSecrets.valueType") }}</th>
              <th>{{ $t("runtimeSecrets.revision") }}</th>
              <th>{{ $t("runtimeSecrets.updatedAt") }}</th>
              <th>
                <span class="sr-only">{{ $t("common.actions") }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="secret in store.items" :key="secret.ref">
              <td>
                <div class="runtime-secrets__identity">
                  <span class="runtime-secrets__icon" aria-hidden="true">
                    <ShieldCheck v-if="secret.state === 'ACTIVE'" :size="18" />
                    <ShieldX v-else :size="18" />
                  </span>
                  <div>
                    <strong>{{ secret.name }}</strong>
                    <p>{{ secret.description || $t("common.noData") }}</p>
                  </div>
                  <StatusBadge :state="secret.state" />
                </div>
              </td>
              <td>
                <code class="runtime-secrets__mask">{{
                  maskedSecretHint(secret)
                }}</code>
              </td>
              <td>{{ $t(`runtimeSecrets.types.${secret.valueType}`) }}</td>
              <td>v{{ secret.currentRevision }}</td>
              <td>{{ formatDate(secret.updatedAt) }}</td>
              <td>
                <div class="runtime-secrets__actions">
                  <button
                    class="icon-button"
                    type="button"
                    :title="$t('runtimeSecrets.reveal')"
                    :aria-label="
                      $t('runtimeSecrets.revealNamed', { name: secret.name })
                    "
                    :disabled="
                      Boolean(store.busyRef) ||
                      !canRuntimeSecretAction(secret, 'REVEAL')
                    "
                    @click="revealTarget = secret"
                  >
                    <Eye :size="17" aria-hidden="true" />
                  </button>
                  <button
                    class="icon-button"
                    type="button"
                    :title="$t('runtimeSecrets.rotate')"
                    :aria-label="
                      $t('runtimeSecrets.rotateNamed', { name: secret.name })
                    "
                    :disabled="
                      Boolean(store.busyRef) ||
                      !canRuntimeSecretAction(secret, 'ROTATE')
                    "
                    @click="openRotate(secret)"
                  >
                    <RotateCw :size="17" aria-hidden="true" />
                  </button>
                  <button
                    class="icon-button icon-button--danger"
                    type="button"
                    :title="$t('runtimeSecrets.revoke')"
                    :aria-label="
                      $t('runtimeSecrets.revokeNamed', { name: secret.name })
                    "
                    :disabled="
                      Boolean(store.busyRef) ||
                      !canRuntimeSecretAction(secret, 'REVOKE')
                    "
                    @click="openRevoke(secret)"
                  >
                    <ShieldX :size="17" aria-hidden="true" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div
          v-if="store.loadingMore"
          class="runtime-secrets__loading"
          role="status"
        >
          {{ $t("common.loading") }}
        </div>
        <button
          v-else-if="store.hasMore"
          class="button runtime-secrets__more"
          type="button"
          @click="store.loadMore"
        >
          {{ $t("runtimeSecrets.loadMore") }}
        </button>
      </div>
    </AsyncState>
  </section>
  <RuntimeSecretRevealDialog
    v-if="revealTarget"
    :secret="revealTarget"
    @close="revealTarget = undefined"
  />
  <RuntimeSecretValueDialog
    v-if="createOpen"
    :busy="store.busyRef === 'create'"
    :problem="store.mutationProblem"
    @close="createOpen = false"
    @create="createSecret"
  />
  <RuntimeSecretValueDialog
    v-if="rotateTarget"
    :secret="rotateTarget"
    :busy="store.busyRef === rotateTarget.ref"
    :problem="store.mutationProblem"
    @close="rotateTarget = undefined"
    @rotate="rotateSecret"
  />
  <RuntimeSecretRevokeDialog
    v-if="revokeTarget"
    :secret="revokeTarget"
    :busy="store.busyRef === revokeTarget.ref"
    :problem="store.mutationProblem"
    @close="revokeTarget = undefined"
    @confirm="revokeSecret"
  />
</template>

<style scoped>
.runtime-secrets {
  padding: 0;
  overflow: hidden;
}
.runtime-secrets__toolbar {
  display: flex;
  min-height: 62px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.runtime-secrets__search {
  display: flex;
  width: min(520px, 100%);
  align-items: center;
  gap: 8px;
}
.runtime-secrets__search input {
  width: 100%;
}
.runtime-secrets__toolbar-meta {
  display: flex;
  align-items: center;
  gap: 14px;
  color: var(--text-secondary);
}
.runtime-secrets__scroll {
  max-height: calc(100dvh - 245px);
  min-height: 360px;
  overflow: auto;
}
.runtime-secrets__table {
  width: 100%;
  min-width: 980px;
  border-collapse: collapse;
}
.runtime-secrets__table th,
.runtime-secrets__table td {
  padding: 13px 14px;
  border-bottom: 1px solid var(--hairline);
  text-align: left;
  vertical-align: middle;
}
.runtime-secrets__table th {
  position: sticky;
  z-index: 1;
  top: 0;
  background: var(--panel);
  color: var(--text-secondary);
  font-size: 13px;
}
.runtime-secrets__identity {
  display: grid;
  min-width: 280px;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}
.runtime-secrets__identity p {
  display: -webkit-box;
  margin: 3px 0 0;
  overflow: hidden;
  color: var(--text-secondary);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.runtime-secrets__icon {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  border-radius: 6px;
  color: var(--accent);
  background: var(--accent-soft);
}
.runtime-secrets__mask {
  white-space: nowrap;
}
.runtime-secrets__actions {
  display: flex;
  justify-content: flex-end;
  gap: 5px;
}
.icon-button--danger {
  color: var(--danger);
}
.runtime-secrets__loading,
.runtime-secrets__more {
  margin: 14px auto;
}
.runtime-secrets__loading {
  width: max-content;
  color: var(--text-secondary);
}
@media (max-width: 760px) {
  .runtime-secrets__toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .runtime-secrets__toolbar-meta {
    justify-content: space-between;
  }
  .runtime-secrets__toolbar-meta .button {
    flex: 0 0 auto;
  }
  .runtime-secrets__scroll {
    max-height: none;
  }
}
</style>
