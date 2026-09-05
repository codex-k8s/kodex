<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import {
  executeGitSource,
  forgetGitSource,
  gitSourceConnection,
  gitSourceConnections,
  pendingGitSource,
  prepareGitSource,
  rememberGitSource,
  type GitSourceAttempt,
} from "./git-source";

const props = defineProps<{
  configuration: ManagedConfiguration;
  disabled?: boolean;
}>();
const emit = defineEmits<{ changed: []; busy: [value: boolean] }>();
const { t } = useI18n();
const open = ref(false);
const working = ref(false);
const awaitingRead = ref(false);
const problem = ref<AppProblem>();
const pending = ref<GitSourceAttempt>();
const selected = ref<AsyncEntityOption>();
const repository = ref("");
const refName = ref("");
const path = ref("");
const format = ref<"JSON" | "YAML">("YAML");
const source = computed(() => props.configuration.gitSource);
function acceptCurrent(): void {
  const attempt = pending.value;
  if (
    !attempt ||
    working.value ||
    props.disabled ||
    props.configuration.version <= attempt.version
  )
    return;
  try {
    forgetGitSource(attempt, sessionStorage);
    pending.value = undefined;
    problem.value = undefined;
  } catch (error) {
    problem.value = asProblem(error);
  }
}
const locked = computed(
  () => working.value || awaitingRead.value || !!pending.value,
);
const controller = new AbortController();
let timer: ReturnType<typeof setTimeout> | undefined;
let polls = 0;
const paused = ref(false);
watch(locked, (value) => emit("busy", value), { immediate: true });
watch(
  () => props.configuration.ref,
  (configurationRef) => {
    try {
      pending.value = pendingGitSource(configurationRef, sessionStorage);
    } catch (error) {
      problem.value = asProblem(error);
      awaitingRead.value = true;
    }
  },
  { immediate: true },
);
watch(
  () => props.configuration,
  () => {
    awaitingRead.value = false;
  },
);
watch(
  () => `${source.value?.ref ?? ""}/${String(source.value?.generation ?? 0)}`,
  () => {
    polls = 0;
    paused.value = false;
  },
);
function schedule(): void {
  clearTimeout(timer);
  if (
    !source.value ||
    !["QUEUED", "CLAIMED"].includes(source.value.state) ||
    controller.signal.aborted
  )
    return;
  if (polls >= 150) {
    paused.value = true;
    return;
  }
  timer = setTimeout(() => {
    if (!props.disabled && !locked.value) {
      polls += 1;
      emit("changed");
    }
    schedule();
  }, 2000);
}
watch(() => source.value?.state, schedule, { immediate: true });
onBeforeUnmount(() => {
  controller.abort();
  clearTimeout(timer);
  emit("busy", false);
});
async function connections(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
) {
  const result = await gitSourceConnections(query, cursor, signal);
  return {
    items: result.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.definitionKey,
      disabled: !["github", "gitlab"].includes(item.definitionKey),
      disabledReason: t("gitSource.providerOnly"),
    })),
    nextPageToken: result.nextPageToken,
  };
}
async function run(configure = false): Promise<void> {
  if (working.value || awaitingRead.value || props.disabled) return;
  working.value = true;
  problem.value = undefined;
  try {
    let attempt = pending.value;
    if (!attempt) {
      if (configure) {
        if (!selected.value)
          throw new Error("Git source connection is required");
        const connection = await gitSourceConnection(
          selected.value.ref,
          controller.signal,
        );
        if (controller.signal.aborted) return;
        attempt = prepareGitSource(props.configuration, {
          connectionRef: connection.ref,
          expectedConnectionVersion: connection.version,
          repositoryRef: repository.value,
          refName: refName.value,
          path: path.value,
          contentFormat: format.value,
        });
      } else attempt = prepareGitSource(props.configuration);
      rememberGitSource(attempt, sessionStorage);
      pending.value = attempt;
    }
    await executeGitSource(attempt, controller.signal);
    if (controller.signal.aborted) return;
    forgetGitSource(attempt, sessionStorage);
    pending.value = undefined;
    awaitingRead.value = true;
    open.value = false;
    polls = 0;
    paused.value = false;
    emit("changed");
  } catch (error) {
    if (!controller.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!controller.signal.aborted) working.value = false;
  }
}
</script>

<template>
  <section
    class="git-source-panel"
    :aria-label="t('gitSource.title')"
    :aria-busy="working"
  >
    <h3>{{ t("gitSource.title") }}</h3>
    <ProblemNotice v-if="problem" :problem="problem" />
    <dl v-if="source">
      <dt>{{ t("gitSource.state") }}</dt>
      <dd>{{ source.state }}</dd>
      <dt>{{ t("gitSource.connection") }}</dt>
      <dd>{{ source.providerKey }} · {{ source.connectionRef }}</dd>
      <dt>{{ t("gitSource.repository") }}</dt>
      <dd>{{ source.repositoryRef }}</dd>
      <dt>{{ t("gitSource.ref") }}</dt>
      <dd>{{ source.refName }}</dd>
      <dt>{{ t("gitSource.path") }}</dt>
      <dd>{{ source.path }}</dd>
      <dt>{{ t("gitSource.generation") }}</dt>
      <dd>{{ source.generation }} / {{ source.version }}</dd>
      <template v-if="source.acceptedCommitSha"
        ><dt>Commit SHA</dt>
        <dd>{{ source.acceptedCommitSha }}</dd></template
      >
      <template v-if="source.acceptedContentSha256"
        ><dt>SHA-256</dt>
        <dd>{{ source.acceptedContentSha256 }}</dd></template
      >
      <template v-if="source.acceptedRevisionRef"
        ><dt>{{ t("gitSource.accepted") }}</dt>
        <dd>{{ source.acceptedRevisionRef }}</dd></template
      >
      <template v-if="source.syncedAt"
        ><dt>{{ t("gitSource.synced") }}</dt>
        <dd>{{ source.syncedAt }}</dd></template
      >
      <template v-if="source.failureCode"
        ><dt>{{ t("gitSource.failure") }}</dt>
        <dd>{{ source.failureCode }}</dd></template
      >
    </dl>
    <p v-if="pending" role="status">{{ t("gitSource.unknown") }}</p>
    <button
      v-if="pending && configuration.version > pending.version"
      class="button"
      :disabled="working || disabled"
      @click="acceptCurrent"
    >
      {{ t("gitSource.acceptCurrent") }}
    </button>
    <p v-if="awaitingRead" role="status">{{ t("gitSource.readback") }}</p>
    <p v-if="paused" role="status">{{ t("gitSource.paused") }}</p>
    <div class="git-source-panel__actions">
      <button
        v-if="pending"
        class="button"
        :disabled="working || disabled || awaitingRead"
        @click="run()"
      >
        {{ t("gitSource.retry") }}
      </button>
      <template v-else>
        <button
          class="button"
          :disabled="locked || disabled"
          @click="open = true"
        >
          {{ t("gitSource.configure") }}
        </button>
        <button
          v-if="configuration.managedBy === 'GIT'"
          class="button"
          :disabled="locked || disabled"
          @click="run()"
        >
          {{ t("gitSource.refresh") }}
        </button>
      </template>
      <button
        class="button"
        :disabled="working || disabled"
        @click="emit('changed')"
      >
        {{ t("common.refresh") }}
      </button>
    </div>
    <ModalDialog
      v-if="open"
      :title="t('gitSource.configure')"
      :busy="working"
      @close="open = false"
    >
      <form class="git-source-panel__form" @submit.prevent="run(true)">
        <p>{{ t("gitSource.transition") }}</p>
        <ProblemNotice v-if="problem" :problem="problem" />
        <AsyncEntityPicker
          :model-value="selected?.ref"
          :selected="selected"
          :load-page="connections"
          :trigger-label="t('gitSource.connection')"
          :disabled="locked"
          @select="selected = $event"
          @update:model-value="!$event && (selected = undefined)"
        />
        <label
          >{{ t("gitSource.repository")
          }}<input
            v-model="repository"
            required
            maxlength="256"
            :disabled="locked"
        /></label>
        <label
          >{{ t("gitSource.ref")
          }}<input
            v-model="refName"
            required
            maxlength="256"
            :disabled="locked"
        /></label>
        <label
          >{{ t("gitSource.path")
          }}<input v-model="path" required maxlength="512" :disabled="locked"
        /></label>
        <label
          >{{ t("managed.format")
          }}<select v-model="format" :disabled="locked">
            <option>JSON</option>
            <option>YAML</option>
          </select></label
        >
        <button class="button" :disabled="locked || disabled || !selected">
          {{ t("gitSource.configure") }}
        </button>
      </form>
    </ModalDialog>
  </section>
</template>

<style scoped>
.git-source-panel {
  min-width: 0;
  padding-block: 1rem;
}
.git-source-panel dl {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 0.5rem;
}
.git-source-panel dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.git-source-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.git-source-panel__form,
.git-source-panel__form label {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
}
.git-source-panel__form input,
.git-source-panel__form select {
  width: 100%;
  min-width: 0;
}
</style>
