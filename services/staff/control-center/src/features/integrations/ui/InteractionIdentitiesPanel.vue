<script setup lang="ts">
import { Plus, RefreshCw, ShieldX } from "@lucide/vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { listAccessSubjects } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  InteractionIdentity,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import {
  createInteractionIdentity,
  readInteractionIdentities,
  removeInteractionIdentity,
  validIdentityInput,
} from "../interaction-identities";

const props = defineProps<{ connection: IntegrationConnection }>();
const { t } = useI18n();
const items = ref<InteractionIdentity[]>([]);
const cursor = ref("");
const loading = ref(false);
const loaded = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const mutationProblem = ref<AppProblem>();
const createOpen = ref(false);
const revokeTarget = ref<InteractionIdentity>();
const input = reactive({
  externalTeamRef: "",
  externalChannelRef: "",
  externalUserDigest: "",
  subjectRef: "",
});
const valid = computed(() => validIdentityInput(input));
let controller: AbortController | undefined;
let generation = 0;

async function load(more = false): Promise<void> {
  if (busy.value || (more && (loading.value || !cursor.value))) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const page = await readInteractionIdentities(
      props.connection.ref,
      more ? cursor.value : undefined,
      active.signal,
    );
    if (current !== generation) return;
    if (
      more &&
      page.items.some((item) => items.value.some((old) => old.ref === item.ref))
    )
      throw new Error("Repeated interaction identity page");
    items.value = more ? [...items.value, ...page.items] : page.items;
    cursor.value = page.nextPageToken;
    loaded.value = true;
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
async function subjects(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listAccessSubjects({
        query: { kind: "USER", query, pageToken, pageSize: 40 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((subject) => ({
      ref: subject.ref,
      title: subject.displayName,
      description: subject.ref,
      disabled: !subject.active || subject.kind !== "USER",
      disabledReason: !subject.active ? t("identity.inactiveUser") : undefined,
    })),
    nextPageToken: page.nextPageToken,
  };
}
function chooseSubject(value: unknown): void {
  input.subjectRef = typeof value === "string" ? value : "";
}
function openCreate(): void {
  Object.assign(input, {
    externalTeamRef: "",
    externalChannelRef: "",
    externalUserDigest: "",
    subjectRef: "",
  });
  mutationProblem.value = undefined;
  createOpen.value = true;
}
async function create(): Promise<void> {
  if (!valid.value || busy.value) return;
  const current = generation;
  busy.value = true;
  mutationProblem.value = undefined;
  try {
    const receipt = await createInteractionIdentity(props.connection, {
      ...input,
    });
    if (current !== generation) return;
    items.value = [
      ...items.value.filter((item) => item.ref !== receipt.ref),
      receipt,
    ];
    createOpen.value = false;
    Object.assign(input, {
      externalTeamRef: "",
      externalChannelRef: "",
      externalUserDigest: "",
      subjectRef: "",
    });
  } catch (error) {
    if (current === generation) mutationProblem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function revoke(): Promise<void> {
  if (!revokeTarget.value || busy.value) return;
  const current = generation;
  busy.value = true;
  mutationProblem.value = undefined;
  try {
    const receipt = await removeInteractionIdentity(revokeTarget.value);
    if (current !== generation) return;
    items.value = items.value.map((item) =>
      item.ref === receipt.ref ? receipt : item,
    );
    revokeTarget.value = undefined;
  } catch (error) {
    if (current === generation) mutationProblem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
watch(
  () => [props.connection.ref, props.connection.version],
  () => {
    items.value = [];
    cursor.value = "";
    loaded.value = false;
    createOpen.value = false;
    revokeTarget.value = undefined;
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
});
</script>
<template>
  <section class="identity-panel">
    <header>
      <h3>{{ t("identity.title") }}</h3>
      <button
        class="icon-button"
        :disabled="loading || busy"
        :title="t('identity.refresh')"
        :aria-label="t('identity.refresh')"
        @click="load()"
      >
        <RefreshCw :size="17" /></button
      ><button
        class="button"
        :disabled="
          !loaded || busy || ['DISABLED', 'DELETED'].includes(connection.state)
        "
        @click="openCreate"
      >
        <Plus :size="17" />{{ t("identity.bind") }}
      </button>
    </header>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ t("common.loading") }}</p>
    <p v-else-if="loaded && !items.length">{{ t("common.empty") }}</p>
    <div class="identity-list">
      <article v-for="item in items" :key="item.ref" class="identity-row">
        <div>
          <strong>{{ item.subjectRef }}</strong
          ><StatusBadge :state="item.state" /><span
            >v{{ item.connectionVersion }}</span
          ><span
            v-if="item.connectionVersion !== connection.version"
            class="identity-stale"
            >{{ t("identity.staleVersion") }}</span
          >
        </div>
        <dl>
          <dt>{{ t("identity.team") }}</dt>
          <dd>{{ item.externalTeamRef }}</dd>
          <dt>{{ t("identity.channel") }}</dt>
          <dd>{{ item.externalChannelRef }}</dd>
          <dt>{{ t("identity.digest") }}</dt>
          <dd>
            <code>{{ item.externalUserDigest }}</code>
          </dd>
        </dl>
        <button
          v-if="item.state === 'ACTIVE'"
          class="icon-button"
          :disabled="busy"
          :title="t('identity.revoke')"
          :aria-label="t('identity.revoke')"
          @click="
            mutationProblem = undefined;
            revokeTarget = item;
          "
        >
          <ShieldX :size="18" />
        </button>
      </article>
      <button
        v-if="cursor"
        class="button"
        :disabled="loading || busy"
        @click="load(true)"
      >
        {{ t("identity.loadMore") }}
      </button>
    </div>
  </section>
  <ModalDialog
    v-if="createOpen"
    :title="t('identity.bind')"
    :busy="busy"
    @close="createOpen = false"
  >
    <div class="identity-form">
      <span>{{
        t("identity.connectionVersion", { version: connection.version })
      }}</span>
      <label
        >{{ t("identity.team")
        }}<input
          v-model="input.externalTeamRef"
          maxlength="128"
          :disabled="busy"
      /></label>
      <label
        >{{ t("identity.channel")
        }}<input
          v-model="input.externalChannelRef"
          maxlength="128"
          :disabled="busy"
      /></label>
      <label
        >{{ t("identity.digest")
        }}<input
          v-model="input.externalUserDigest"
          maxlength="64"
          :disabled="busy"
          autocomplete="off"
          spellcheck="false"
      /></label>
      <AsyncEntityPicker
        :model-value="input.subjectRef || null"
        :load-page="subjects"
        :trigger-label="t('identity.subject')"
        :disabled="busy"
        @update:model-value="chooseSubject"
      />
      <ProblemNotice
        v-if="mutationProblem"
        :problem="mutationProblem"
        compact
      />
    </div>
    <template #actions
      ><button class="button" :disabled="busy" @click="createOpen = false">
        {{ t("common.cancel") }}</button
      ><button
        class="button button--primary"
        :disabled="busy || !valid"
        @click="create"
      >
        <Plus :size="17" />{{ t("identity.bind") }}
      </button></template
    >
  </ModalDialog>
  <ModalDialog
    v-if="revokeTarget"
    :title="t('identity.revoke')"
    :busy="busy"
    @close="revokeTarget = undefined"
  >
    <p>{{ revokeTarget.subjectRef }}</p>
    <p>
      {{ revokeTarget.externalTeamRef }} / {{ revokeTarget.externalChannelRef }}
    </p>
    <ProblemNotice v-if="mutationProblem" :problem="mutationProblem" compact />
    <template #actions
      ><button
        class="button"
        :disabled="busy"
        @click="revokeTarget = undefined"
      >
        {{ t("common.cancel") }}</button
      ><button class="button button--danger" :disabled="busy" @click="revoke">
        <ShieldX :size="17" />{{ t("identity.revoke") }}
      </button></template
    >
  </ModalDialog>
</template>
<style scoped>
.identity-panel {
  min-width: 0;
}
.identity-panel > header,
.identity-row > div {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}
.identity-panel h3 {
  font-size: 16px;
  margin-right: auto;
}
.identity-list {
  max-height: 1080px;
  overflow: auto;
}
.identity-row {
  position: relative;
  min-height: 180px;
  padding: 12px 44px 12px 0;
  border-top: 1px solid var(--border);
  overflow-wrap: anywhere;
}
.identity-row > button {
  position: absolute;
  top: 12px;
  right: 0;
}
.identity-row dl {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 8px;
}
.identity-row dd {
  margin: 0;
}
.identity-form,
.identity-form label {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.identity-stale {
  color: var(--danger);
}
</style>
