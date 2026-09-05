<script setup lang="ts">
import {
  Eye,
  Link2,
  Maximize2,
  Plus,
  RotateCw,
  Search,
  ShieldCheck,
  ShieldX,
} from "@lucide/vue";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useSessionStore } from "@/features/session/store";
import SecretImpactDialog from "@/features/runtime/SecretImpactDialog.vue";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { readRuntimeSecret } from "./api";

import type { RuntimeSecret } from "./model";
import { canRuntimeSecretAction, maskedSecretHint } from "./model";
import RuntimeSecretRevealDialog from "./RuntimeSecretRevealDialog.vue";
import RuntimeSecretRevokeDialog from "./RuntimeSecretRevokeDialog.vue";
import RuntimeSecretDraftDialog from "./RuntimeSecretDraftDialog.vue";
import type { RuntimeSecretDraft } from "./draft-api";
import { useRuntimeSecretsStore } from "./store";

const props = defineProps<{
  projectRef: string;
  initialSecretRef?: string;
  initialDraftRef?: string;
  initialPlanRef?: string;
}>();
const emit = defineEmits<{
  draftSaved: [draftRef: string];
  planPrepared: [draftRef: string, planRef: string];
}>();
function planPrepared(draftRef: string, planRef: string): void {
  emit("planPrepared", draftRef, planRef);
}
const resumeOpen = ref(false);
function draftSaved(draft: RuntimeSecretDraft): void {
  emit("draftSaved", draft.ref);
  if (draft.state !== "PUBLISHED") void store.reload();
}
const store = useRuntimeSecretsStore();
const session = useSessionStore();
const { locale } = useI18n();
const search = ref("");
const createOpen = ref(false);
const expanded = ref(false);
const rotateTarget = ref<RuntimeSecret>();
const revealTarget = ref<RuntimeSecret>();
const revokeTarget = ref<RuntimeSecret>();
const details = ref<RuntimeSecret>();
const impactTarget = ref<RuntimeSecret>();
const detailsProblem = ref<AppProblem>();
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
  details.value = undefined;
}

function openRevoke(secret: RuntimeSecret): void {
  if (!canRuntimeSecretAction(secret, "REVOKE")) return;
  prepareMutation();
  revokeTarget.value = secret;
  details.value = undefined;
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

watch(
  () => props.initialDraftRef,
  (value) => {
    if (value && !createOpen.value && !rotateTarget.value)
      resumeOpen.value = true;
  },
  { immediate: true },
);
watch(search, (value) => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void store.load(props.projectRef, value), 500);
});
watch(
  () => [props.projectRef, props.initialSecretRef],
  async (_value, _previous, cleanup) => {
    details.value = undefined;
    detailsProblem.value = undefined;
    if (!props.initialSecretRef) return;
    const controller = new AbortController();
    cleanup(() => controller.abort());
    try {
      const secret = await readRuntimeSecret(
        props.initialSecretRef,
        props.projectRef,
        controller.signal,
      );
      if (!controller.signal.aborted) details.value = secret;
    } catch (error) {
      if (!controller.signal.aborted) detailsProblem.value = asProblem(error);
    }
  },
  { immediate: true },
);
watch(
  () => props.projectRef,
  (value) => {
    createOpen.value = false;
    rotateTarget.value = undefined;
    revealTarget.value = undefined;
    revokeTarget.value = undefined;
    expanded.value = false;
    if (searchTimer) clearTimeout(searchTimer);
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
  <component
    :is="expanded ? ModalDialog : 'section'"
    class="runtime-secrets"
    :class="{ 'runtime-secrets--expanded': expanded }"
    :title="expanded ? $t('runtimeSecrets.secret') : undefined"
    size="full"
    @close="expanded = false"
  >
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
        <button
          v-if="!expanded"
          class="icon-button"
          type="button"
          :title="$t('catalog.expand')"
          :aria-label="$t('catalog.expand')"
          @click="expanded = true"
        >
          <Maximize2 :size="17" />
        </button>
        <span>{{
          $t("runtimeSecrets.shown", { count: store.items.length })
        }}</span>
        <button
          v-if="initialDraftRef"
          class="button"
          :disabled="createOpen || Boolean(rotateTarget)"
          @click="resumeOpen = true"
        >
          {{ $t("runtimeSecrets.draft.resume") }}
        </button>
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
    <ProblemNotice v-if="detailsProblem" :problem="detailsProblem" compact />

    <AsyncState
      :loading="store.loading && !store.items.length"
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
        @retry="store.reload"
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
                    <button
                      class="runtime-secrets__name"
                      @click="details = secret"
                    >
                      {{ secret.name }}
                    </button>
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
  </component>
  <ModalDialog
    v-if="details"
    :title="details.name"
    @close="details = undefined"
  >
    <div class="runtime-secret-details">
      <StatusBadge :state="details.state" />
      <p>{{ details.description }}</p>
      <dl>
        <dt>{{ $t("runtimeSecrets.valueType") }}</dt>
        <dd>{{ $t(`runtimeSecrets.types.${details.valueType}`) }}</dd>
        <dt>{{ $t("runtimeSecrets.revision") }}</dt>
        <dd>v{{ details.currentRevision }}</dd>
        <dt>{{ $t("runtimeSecrets.maskedHint") }}</dt>
        <dd>{{ maskedSecretHint(details) }}</dd>
        <dt>{{ $t("runtimeSecrets.updatedAt") }}</dt>
        <dd>{{ formatDate(details.updatedAt) }}</dd>
      </dl>
      <div class="runtime-secrets__actions">
        <button
          class="icon-button"
          :title="$t('impact.inspect')"
          :aria-label="$t('impact.inspect')"
          @click="
            impactTarget = details;
            details = undefined;
          "
        >
          <Link2 :size="18" />
        </button>
        <button
          v-if="canRuntimeSecretAction(details, 'REVEAL')"
          class="icon-button"
          :title="$t('runtimeSecrets.reveal')"
          :aria-label="$t('runtimeSecrets.reveal')"
          @click="
            revealTarget = details;
            details = undefined;
          "
        >
          <Eye :size="18" />
        </button>
        <button
          v-if="canRuntimeSecretAction(details, 'ROTATE')"
          class="icon-button"
          :title="$t('runtimeSecrets.rotate')"
          :aria-label="$t('runtimeSecrets.rotate')"
          @click="openRotate(details)"
        >
          <RotateCw :size="18" />
        </button>
        <button
          v-if="canRuntimeSecretAction(details, 'REVOKE')"
          class="icon-button icon-button--danger"
          :title="$t('runtimeSecrets.revoke')"
          :aria-label="$t('runtimeSecrets.revoke')"
          @click="openRevoke(details)"
        >
          <ShieldX :size="18" />
        </button>
      </div>
    </div>
  </ModalDialog>
  <SecretImpactDialog
    v-if="impactTarget"
    :key="`${impactTarget.ref}:${impactTarget.currentRevision}`"
    :secret-ref="impactTarget.ref"
    :revision="impactTarget.currentRevision"
    @close="impactTarget = undefined"
  />
  <RuntimeSecretRevealDialog
    v-if="revealTarget"
    :secret="revealTarget"
    @close="revealTarget = undefined"
  />
  <RuntimeSecretDraftDialog
    v-if="createOpen"
    :project-ref="projectRef"
    @close="createOpen = false"
    @saved="draftSaved"
    @published="store.acceptPublication"
    @plan-prepared="planPrepared"
  />
  <RuntimeSecretDraftDialog
    v-if="rotateTarget"
    :secret="rotateTarget"
    :project-ref="projectRef"
    @close="rotateTarget = undefined"
    @saved="draftSaved"
    @published="store.acceptPublication"
    @plan-prepared="planPrepared"
  />
  <RuntimeSecretDraftDialog
    v-if="resumeOpen && initialDraftRef"
    :key="JSON.stringify([initialDraftRef, initialPlanRef])"
    :project-ref="projectRef"
    :initial-draft-ref="initialDraftRef"
    :initial-plan-ref="initialPlanRef"
    @close="resumeOpen = false"
    @saved="draftSaved"
    @published="store.acceptPublication"
    @plan-prepared="planPrepared"
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
.runtime-secrets__name {
  border: 0;
  padding: 0;
  background: none;
  color: inherit;
  font-weight: 600;
  text-align: left;
  overflow-wrap: anywhere;
}
.runtime-secrets__name:hover {
  color: var(--accent);
}
.runtime-secret-details {
  overflow-wrap: anywhere;
}
.runtime-secret-details dl {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 16px;
}
.runtime-secret-details dd {
  margin: 0;
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
  max-height: 526px;
  overflow: auto;
}
.runtime-secrets--expanded .runtime-secrets__scroll {
  max-height: calc(100dvh - 220px);
}
.runtime-secrets__table tbody tr {
  height: 80px;
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
  .runtime-secrets__table {
    min-width: 0;
    display: block;
  }
  .runtime-secrets__table thead {
    display: none;
  }
  .runtime-secrets__table tbody {
    display: grid;
  }
  .runtime-secrets__table tr {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
  }
  .runtime-secrets__table td {
    min-width: 0;
    border: 0;
    padding: 6px 14px;
    overflow-wrap: anywhere;
  }
  .runtime-secrets__table td:first-child,
  .runtime-secrets__table td:last-child {
    grid-column: 1 / -1;
  }
  .runtime-secrets__identity {
    min-width: 0;
  }
  .runtime-secrets__mask {
    white-space: normal;
    overflow-wrap: anywhere;
  }
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
    max-height: 1512px;
  }
  .runtime-secrets__table tbody tr {
    height: 252px;
    grid-template-rows: minmax(0, 1fr) 30px 30px 44px;
  }
  .runtime-secrets__name {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }
  .runtime-secrets__identity {
    grid-template-columns: 34px minmax(0, 1fr);
  }
  .runtime-secrets__identity > :last-child {
    grid-column: 2;
    justify-self: start;
  }
}
</style>
