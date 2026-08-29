<script setup lang="ts">
import {
  Archive,
  Box,
  Hammer,
  History,
  Link2,
  PackageCheck,
  RotateCcw,
  ShieldCheck,
  TerminalSquare,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import RoleImageContractGap from "@/features/role-images/RoleImageContractGap.vue";
import RoleImageDockerfileEditor from "@/features/role-images/RoleImageDockerfileEditor.vue";
import {
  canRequestBuild,
  defaultDockerfile,
  latestBuild,
  roleImageApiGaps,
  roleImageState,
  validateDockerfile,
} from "@/features/role-images/model";
import { useRoleImagesStore } from "@/features/role-images/store";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  projectRef: string;
  recipeRef?: string;
}>();
const { t } = useI18n();
const store = useRoleImagesStore();
const name = ref("");
const roleDefinitionRef = ref("");
const dockerfile = ref(defaultDockerfile());
const recipe = computed(() =>
  props.recipeRef ? store.recipes[props.recipeRef] : undefined,
);
const builds = computed(() =>
  props.recipeRef ? (store.builds[props.recipeRef] ?? []) : [],
);
const currentBuild = computed(() => latestBuild(builds.value));
const dockerfileMessages = computed(() =>
  validateDockerfile(dockerfile.value).map((key) => t(key)),
);
const roleLabel = computed(
  () =>
    store.roleDefinitionByRef.get(
      recipe.value?.roleDefinitionRef ?? roleDefinitionRef.value,
    )?.label ?? t("roleImages.unknownRole"),
);
const environmentLabel = computed(() => {
  const key = recipe.value?.environment.environmentKey;
  if (!key) return t("common.noData");
  const environment = store.environmentByKey.get(key);
  return environment ? t(environment.nameMessageKey) : key;
});
const evidenceGap = computed(() =>
  roleImageApiGaps.filter((gap) => gap.key === "evidence"),
);
const executableGap = computed(() =>
  roleImageApiGaps.filter((gap) => gap.key === "executables"),
);
const environmentLinksGap = computed(() =>
  roleImageApiGaps.filter((gap) => gap.key === "environment-links"),
);

function sync(): void {
  if (!recipe.value) return;
  name.value = recipe.value.name;
  roleDefinitionRef.value = recipe.value.roleDefinitionRef;
  // Текущий API не возвращает исходный Dockerfile. Не подменяем его legacy spec.
  dockerfile.value = "";
}

async function load(): Promise<void> {
  const tasks: Promise<void>[] = [
    store.loadSupportingCatalogs(props.projectRef),
  ];
  if (props.recipeRef)
    tasks.push(store.loadDetail(props.projectRef, props.recipeRef));
  await Promise.all(tasks);
  sync();
}

async function runCommand(
  action: "REQUEST_BUILD" | "ARCHIVE" | "RESTORE",
): Promise<void> {
  if (!recipe.value) return;
  if (
    action !== "REQUEST_BUILD" &&
    !window.confirm(
      t(
        action === "ARCHIVE"
          ? "roleImages.confirmArchive"
          : "roleImages.confirmRestore",
      ),
    )
  )
    return;
  try {
    await store.command(props.projectRef, recipe.value, action);
    sync();
  } catch {
    // Store сохраняет нормализованную problem-модель для видимого состояния.
  }
}

watch(
  () => [props.projectRef, props.recipeRef],
  () => void load(),
);
onMounted(() => void load());
onBeforeUnmount(() => store.dispose());
</script>

<template>
  <div class="role-image-editor">
    <ProblemNotice
      v-if="store.problem"
      :problem="store.problem"
      @retry="load"
    />

    <div v-if="store.loadingDetail" class="editor-loading" role="status">
      {{ t("common.loading") }}
    </div>
    <template v-else>
      <section class="panel image-summary">
        <div class="image-summary__identity">
          <span class="image-summary__icon"><Box :size="22" /></span>
          <div>
            <span class="eyebrow">{{ t("roleImages.entity") }}</span>
            <h2>{{ recipe?.name ?? t("roleImages.new") }}</h2>
            <p>{{ roleLabel }}</p>
          </div>
        </div>
        <StatusBadge
          v-if="recipe"
          :state="roleImageState(recipe, currentBuild)"
          :label="
            recipe.promotedImageReady && currentBuild?.stage === 'COMPLETED'
              ? t('roleImages.promoted')
              : undefined
          "
        />
        <div v-if="recipe" class="image-summary__actions">
          <button
            v-if="canRequestBuild(recipe)"
            class="button button--primary"
            type="button"
            :disabled="store.mutating"
            @click="runCommand('REQUEST_BUILD')"
          >
            <Hammer :size="16" aria-hidden="true" />
            {{ t("roleImages.requestBuild") }}
          </button>
          <button
            v-if="recipe.nextActions.includes('ARCHIVE')"
            class="button"
            type="button"
            :disabled="store.mutating"
            @click="runCommand('ARCHIVE')"
          >
            <Archive :size="16" aria-hidden="true" />
            {{ t("common.archive") }}
          </button>
          <button
            v-if="recipe.nextActions.includes('RESTORE')"
            class="button"
            type="button"
            :disabled="store.mutating"
            @click="runCommand('RESTORE')"
          >
            <RotateCcw :size="16" aria-hidden="true" />
            {{ t("roleImages.restore") }}
          </button>
        </div>
      </section>

      <div class="editor-layout">
        <main class="editor-main">
          <section class="panel recipe-form">
            <header class="section-header">
              <div>
                <h2>{{ t("roleImages.sourceTitle") }}</h2>
                <p>{{ t("roleImages.sourceHelp") }}</p>
              </div>
              <StatusBadge
                :state="recipe ? 'UNAVAILABLE' : 'DRAFT'"
                :label="
                  recipe ? t('common.unavailable') : t('roleImages.localDraft')
                "
              />
            </header>
            <div class="recipe-fields">
              <label class="field">
                <span>{{ t("common.name") }}</span>
                <input v-model="name" maxlength="120" :readonly="!!recipe" />
              </label>
              <label class="field">
                <span>{{ t("roleImages.role") }}</span>
                <select
                  v-model="roleDefinitionRef"
                  :disabled="!!recipe || !store.roleDefinitions.length"
                >
                  <option value="" disabled>
                    {{ t("roleImages.chooseRole") }}
                  </option>
                  <option
                    v-for="role in store.roleDefinitions"
                    :key="role.ref"
                    :value="role.ref"
                  >
                    {{ role.label }} ·
                    {{
                      t("roleImages.agentsCount", { count: role.agentCount })
                    }}
                  </option>
                </select>
              </label>
            </div>
            <RoleImageDockerfileEditor
              v-model="dockerfile"
              :label="t('roleImages.dockerfile')"
              :readonly="!!recipe"
              :validation-messages="recipe ? [] : dockerfileMessages"
            />
            <div class="save-boundary">
              <p>{{ t("roleImages.saveBlocked") }}</p>
              <button
                class="button button--primary"
                type="button"
                disabled
                :title="t('roleImages.saveBlocked')"
              >
                {{
                  recipe ? t("roleImages.createRevision") : t("common.create")
                }}
              </button>
            </div>
            <RoleImageContractGap
              :gaps="
                roleImageApiGaps.filter((gap) =>
                  ['dockerfile', 'revisions'].includes(gap.key),
                )
              "
            />
          </section>

          <section v-if="recipe" class="panel build-history">
            <header class="section-header">
              <div>
                <h2>{{ t("roleImages.buildHistory") }}</h2>
                <p>{{ t("roleImages.buildHistoryHelp") }}</p>
              </div>
              <History :size="20" aria-hidden="true" />
            </header>
            <div v-if="!builds.length" class="empty-section">
              {{ t("roleImages.noBuilds") }}
            </div>
            <article
              v-for="build in builds"
              v-else
              :key="build.ref"
              class="build-row"
            >
              <div>
                <strong>
                  {{ t("roleImages.attempt", { attempt: build.attempt }) }}
                </strong>
                <small>{{ new Date(build.updatedAt).toLocaleString() }}</small>
              </div>
              <div class="build-progress">
                <span :style="{ width: `${build.progressPercent}%` }" />
              </div>
              <span>{{ build.progressPercent }}%</span>
              <StatusBadge :state="build.stage" />
              <p v-if="build.diagnosticSummary" class="build-diagnostic">
                {{ build.diagnosticSummary }}
                <code v-if="build.diagnosticCode">
                  {{ build.diagnosticCode }}
                </code>
              </p>
            </article>
          </section>
        </main>

        <aside class="editor-aside">
          <section class="panel facts-panel">
            <h2>{{ t("roleImages.currentState") }}</h2>
            <dl>
              <div>
                <dt>{{ t("roleImages.generation") }}</dt>
                <dd>{{ recipe?.generation ?? "—" }}</dd>
              </div>
              <div>
                <dt>{{ t("roleImages.environment") }}</dt>
                <dd>{{ environmentLabel }}</dd>
              </div>
              <div>
                <dt>{{ t("roleImages.promotion") }}</dt>
                <dd>
                  {{
                    recipe?.promotedImageReady
                      ? t("roleImages.promoted")
                      : t("roleImages.notPromoted")
                  }}
                </dd>
              </div>
              <div>
                <dt>{{ t("roleImages.updatedAt") }}</dt>
                <dd>
                  {{
                    recipe
                      ? new Date(recipe.updatedAt).toLocaleString()
                      : t("common.noData")
                  }}
                </dd>
              </div>
            </dl>
          </section>

          <section class="panel unavailable-card">
            <ShieldCheck :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.evidence") }}</h2>
            <RoleImageContractGap :gaps="evidenceGap" compact />
          </section>
          <section class="panel unavailable-card">
            <TerminalSquare :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.executables") }}</h2>
            <RoleImageContractGap :gaps="executableGap" compact />
          </section>
          <section class="panel unavailable-card">
            <Link2 :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.usedByEnvironments") }}</h2>
            <RoleImageContractGap :gaps="environmentLinksGap" compact />
            <RouterLink
              class="button"
              :to="`/projects/${encodeURIComponent(projectRef)}/environments`"
            >
              <PackageCheck :size="16" aria-hidden="true" />
              {{ t("roleImages.openEnvironments") }}
            </RouterLink>
          </section>
        </aside>
      </div>
    </template>
  </div>
</template>

<style scoped>
.role-image-editor,
.editor-main,
.editor-aside,
.recipe-form,
.build-history {
  display: grid;
  gap: 16px;
}
.editor-loading {
  display: grid;
  min-height: 420px;
  place-items: center;
}
.image-summary {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 16px;
}
.image-summary__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 12px;
}
.image-summary__identity h2,
.image-summary__identity p {
  margin: 0;
  overflow-wrap: anywhere;
}
.image-summary__identity p {
  color: var(--text-secondary);
}
.image-summary__icon {
  display: grid;
  width: 44px;
  height: 44px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 8px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.image-summary__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
.editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.3fr);
  align-items: start;
  gap: 16px;
}
.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.section-header h2,
.section-header p {
  margin: 0;
}
.section-header p {
  margin-top: 4px;
  color: var(--text-secondary);
}
.recipe-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(240px, 0.45fr);
  gap: 12px;
}
.field {
  display: grid;
  gap: 6px;
}
.field > span {
  font-weight: 600;
}
.save-boundary {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}
.save-boundary p {
  max-width: 680px;
  margin: 0 auto 0 0;
  color: var(--text-secondary);
}
.build-history {
  gap: 0;
}
.build-history > header {
  padding-bottom: 14px;
}
.build-row {
  display: grid;
  grid-template-columns: minmax(150px, 0.35fr) minmax(120px, 1fr) auto auto;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-top: 1px solid var(--border);
}
.build-row > div:first-child {
  display: grid;
}
.build-row small {
  color: var(--text-secondary);
}
.build-progress {
  height: 7px;
  overflow: hidden;
  border-radius: 99px;
  background: var(--canvas);
}
.build-progress span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--accent);
}
.build-diagnostic {
  grid-column: 1 / -1;
  padding: 10px;
  margin: 0;
  border-radius: 6px;
  background: var(--danger-soft);
  color: var(--danger);
}
.build-diagnostic code {
  display: block;
  margin-top: 4px;
}
.empty-section {
  padding: 28px;
  border-top: 1px solid var(--border);
  color: var(--text-secondary);
  text-align: center;
}
.facts-panel h2,
.unavailable-card h2 {
  margin: 0;
  font-size: 1rem;
}
.facts-panel dl {
  display: grid;
  gap: 12px;
  margin: 14px 0 0;
}
.facts-panel dl > div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
}
.facts-panel dt {
  color: var(--text-secondary);
}
.facts-panel dd {
  margin: 0;
  text-align: right;
  overflow-wrap: anywhere;
}
.unavailable-card {
  display: grid;
  gap: 10px;
}
.unavailable-card > svg {
  color: var(--accent-strong);
}
@media (max-width: 1000px) {
  .editor-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .editor-aside {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .facts-panel {
    grid-column: 1 / -1;
  }
}
@media (max-width: 720px) {
  .image-summary {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .image-summary__actions {
    grid-column: 1 / -1;
  }
  .recipe-fields,
  .editor-aside {
    grid-template-columns: minmax(0, 1fr);
  }
  .build-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .build-progress {
    grid-column: 1 / -1;
    grid-row: 2;
  }
  .save-boundary {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
