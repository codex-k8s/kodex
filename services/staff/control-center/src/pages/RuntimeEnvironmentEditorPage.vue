<script setup lang="ts">
import { Plus, Trash2 } from "@lucide/vue";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  emptySecretDescriptor,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import { useRuntimeStore } from "@/features/runtime/store";
import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const runtime = useRuntimeStore();
const projectRef = computed(() => String(route.params.projectRef));
const environmentRef = computed(() => {
  const value = route.params.environmentRef;
  return typeof value === "string" ? value : undefined;
});
const current = computed<RuntimeEnvironmentSet | undefined>(() =>
  environmentRef.value ? runtime.environments[environmentRef.value] : undefined,
);
const versions = computed(() =>
  environmentRef.value
    ? (runtime.environmentVersions[environmentRef.value] ?? [])
    : [],
);
const busy = ref(false);
const problem = ref<AppProblem>();
const input = reactive<RuntimeEnvironmentInput>({
  name: "",
  description: "",
  values: [],
  secretDescriptors: [],
});
const validation = computed(() => validateEnvironmentInput(input));

function sync(value = current.value): void {
  if (!value) return;
  input.name = value.name;
  input.description = value.description;
  input.values = value.currentVersion.values.map((item) => ({ ...item }));
  input.secretDescriptors = value.currentVersion.secretDescriptors.map(
    (item) => ({ ...item }),
  );
}

function addValue(): void {
  input.values.push({ name: "", value: "" });
}

function addSecret(): void {
  input.secretDescriptors.push(emptySecretDescriptor());
}

async function load(): Promise<void> {
  if (!environmentRef.value) return;
  await Promise.all([
    runtime.loadEnvironment(environmentRef.value),
    runtime.loadEnvironmentVersions(environmentRef.value),
  ]);
  sync();
}

async function save(): Promise<void> {
  if (validation.value.length) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const payload: RuntimeEnvironmentInput = {
      name: input.name.trim(),
      description: input.description.trim(),
      values: input.values.map((item) => ({
        name: item.name.trim(),
        value: item.value,
      })),
      secretDescriptors: input.secretDescriptors.map((item) => ({
        name: item.name.trim(),
        secretName: item.secretName.trim(),
        secretKey: item.secretKey.trim(),
        secretUid: item.secretUid.trim(),
        secretResourceVersion: item.secretResourceVersion.trim(),
        contentSha256: item.contentSha256.trim(),
      })),
    };
    const saved = current.value
      ? await runtime.publishEnvironment(current.value, payload)
      : await runtime.createEnvironment(projectRef.value, payload);
    if (!environmentRef.value) {
      await router.replace(
        `/projects/${encodeURIComponent(projectRef.value)}/environments/${encodeURIComponent(saved.ref)}`,
      );
    } else {
      await runtime.loadEnvironmentVersions(saved.ref);
      sync(saved);
    }
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function rollback(versionRef: string): Promise<void> {
  if (!current.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const saved = await runtime.restoreEnvironment(current.value, versionRef);
    await runtime.loadEnvironmentVersions(saved.ref);
    sync(saved);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

watch(environmentRef, () => void load());
onMounted(() => void load());
</script>

<template>
  <PageFrame
    :title="current?.name ?? $t('runtime.newEnvironment')"
    :subtitle="$t('runtime.environmentEditorSubtitle')"
  >
    <template #actions>
      <RouterLink
        class="button"
        :to="`/projects/${encodeURIComponent(projectRef)}/environments`"
      >
        {{ $t("common.cancel") }}
      </RouterLink>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy || validation.length > 0"
        @click="save"
      >
        {{ current ? $t("runtime.publishRevision") : $t("common.create") }}
      </button>
    </template>
    <AsyncState
      :loading="
        environmentRef
          ? runtime.loading[`environment:${environmentRef}`]
          : false
      "
      :problem="
        environmentRef
          ? runtime.problems[`environment:${environmentRef}`]
          : undefined
      "
      @retry="load"
    >
      <div class="environment-editor-layout">
        <form class="panel environment-editor" @submit.prevent="save">
          <section class="editor-section">
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.environmentGeneral") }}</h2>
                <p>{{ $t("runtime.environmentGeneralHelp") }}</p>
              </div>
              <StatusBadge v-if="current" :state="current.state" />
            </div>
            <label class="field">
              <span>{{ $t("common.name") }}</span>
              <input v-model="input.name" required maxlength="120" />
            </label>
            <label class="field">
              <span>{{ $t("common.description") }}</span>
              <textarea v-model="input.description" maxlength="1000" />
            </label>
          </section>

          <section class="editor-section">
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.variables") }}</h2>
                <p>{{ $t("runtime.variablesHelp") }}</p>
              </div>
              <button class="button" type="button" @click="addValue">
                <Plus :size="15" aria-hidden="true" />
                {{ $t("runtime.addVariable") }}
              </button>
            </div>
            <div v-if="input.values.length" class="environment-fields">
              <div
                v-for="(item, index) in input.values"
                :key="index"
                class="environment-field-row"
              >
                <label class="field">
                  <span>{{ $t("runtime.variableName") }}</span>
                  <input v-model="item.name" placeholder="VAR_NAME" />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.nonSecretValue") }}</span>
                  <input v-model="item.value" maxlength="8192" />
                </label>
                <button
                  class="icon-button"
                  type="button"
                  :aria-label="$t('common.delete')"
                  @click="input.values.splice(index, 1)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
            </div>
            <p v-else class="secondary-text">{{ $t("common.empty") }}</p>
          </section>

          <section class="editor-section">
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.secretDescriptors") }}</h2>
                <p>{{ $t("runtime.secretDescriptorsHelp") }}</p>
              </div>
              <button class="button" type="button" @click="addSecret">
                <Plus :size="15" aria-hidden="true" />
                {{ $t("runtime.addSecretDescriptor") }}
              </button>
            </div>
            <div class="secret-warning" role="note">
              {{ $t("runtime.secretValuesForbidden") }}
            </div>
            <article
              v-for="(item, index) in input.secretDescriptors"
              :key="index"
              class="secret-descriptor"
            >
              <div class="section-header">
                <strong>{{
                  $t("runtime.secretDescriptor", { number: index + 1 })
                }}</strong>
                <button
                  class="icon-button"
                  type="button"
                  :aria-label="$t('common.delete')"
                  @click="input.secretDescriptors.splice(index, 1)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
              <div class="secret-grid">
                <label class="field">
                  <span>{{ $t("runtime.variableName") }}</span>
                  <input v-model="item.name" placeholder="SECRET_NAME" />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.secretName") }}</span>
                  <input v-model="item.secretName" />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.secretKey") }}</span>
                  <input v-model="item.secretKey" />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.secretUid") }}</span>
                  <input v-model="item.secretUid" />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.secretResourceVersion") }}</span>
                  <input v-model="item.secretResourceVersion" />
                </label>
                <label class="field field--wide">
                  <span>SHA-256</span>
                  <input
                    v-model="item.contentSha256"
                    maxlength="64"
                    class="mono-input"
                  />
                </label>
              </div>
            </article>
          </section>

          <ul v-if="validation.length" class="validation-list" role="alert">
            <li
              v-for="item in validation"
              :key="`${item.field}:${item.message}`"
            >
              {{ $t(item.message) }}
            </li>
          </ul>
          <ProblemNotice v-if="problem" :problem="problem" />
          <section v-if="problem?.kind === 'conflict'" class="conflict-panel">
            <p>{{ $t("runtime.environmentConflict") }}</p>
            <button class="button" type="button" @click="load">
              {{ $t("runtime.reload") }}
            </button>
          </section>
        </form>

        <aside v-if="current" class="panel revision-panel">
          <div class="section-header">
            <h2>{{ $t("runtime.revisionHistory") }}</h2>
            <span>rev {{ current.currentVersion.revision }}</span>
          </div>
          <article v-for="version in versions" :key="version.ref">
            <div>
              <strong>rev {{ version.revision }}</strong>
              <small>{{ new Date(version.createdAt).toLocaleString() }}</small>
            </div>
            <button
              v-if="version.ref !== current.currentVersion.ref"
              class="button"
              type="button"
              :disabled="busy"
              @click="rollback(version.ref)"
            >
              {{ $t("runtime.rollback") }}
            </button>
            <StatusBadge v-else state="ACTIVE" />
          </article>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.environment-editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(270px, 0.32fr);
  gap: 16px;
  align-items: start;
}
.environment-editor {
  display: grid;
  gap: 20px;
}
.editor-section {
  display: grid;
  gap: 12px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}
.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}
.section-header p {
  margin-bottom: 0;
  color: var(--text-secondary);
}
.environment-fields,
.secret-grid {
  display: grid;
  gap: 10px;
}
.environment-field-row {
  display: grid;
  grid-template-columns: minmax(180px, 0.45fr) minmax(240px, 1fr) 36px;
  gap: 10px;
  align-items: end;
}
.secret-descriptor {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.secret-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.secret-warning,
.conflict-panel {
  padding: 10px 12px;
  border: 1px solid var(--warning);
  border-radius: 7px;
  background: var(--warning-soft);
  color: var(--warning);
}
.mono-input {
  font-family: var(--font-mono);
}
.validation-list {
  padding: 12px 12px 12px 30px;
  border: 1px solid var(--danger);
  border-radius: 7px;
  background: var(--danger-soft);
  color: var(--danger);
}
.revision-panel {
  display: grid;
  gap: 0;
}
.revision-panel > article {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 0;
  border-bottom: 1px solid var(--hairline);
}
.revision-panel > article > div,
.revision-panel small {
  display: block;
}
.revision-panel small,
.secondary-text {
  color: var(--text-secondary);
}
@media (max-width: 960px) {
  .environment-editor-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 660px) {
  .environment-field-row,
  .secret-grid {
    grid-template-columns: 1fr;
  }
}
</style>
