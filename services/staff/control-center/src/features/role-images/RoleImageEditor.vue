<script setup lang="ts">
import {
  Archive,
  Box,
  Hammer,
  Maximize2,
  Link2,
  PackageCheck,
  RotateCcw,
  ShieldCheck,
  TerminalSquare,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import RoleImageDockerfileEditor from "@/features/role-images/RoleImageDockerfileEditor.vue";
import RoleImageLineage from "./RoleImageLineage.vue";
import {
  buildIsActive,
  buildRevisionIdentity,
  canPromoteRoleImage,
  canRequestBuild,
  latestBuild,
  roleImageState,
  validateDockerfile,
} from "@/features/role-images/model";
import { useRoleImagesStore } from "@/features/role-images/store";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import CodeDiff from "@/shared/ui/CodeDiff.vue";
import CodeEditor from "@/shared/ui/CodeEditor.vue";

const props = defineProps<{
  projectRef: string;
  recipeRef?: string;
}>();
const { t } = useI18n();
const router = useRouter();
const store = useRoleImagesStore();
const name = ref("");
const roleDefinitionRef = ref("");
const environmentKey = ref("");
const dockerfile = ref("");
const diffOpen = ref(false);
const buildsExpanded = ref(false);
const revisionsExpanded = ref(false);
const openedBuildSources = ref(new Set<string>());
function toggleBuildSource(ref: string, event: Event): void {
  const details = event.currentTarget;
  if (!(details instanceof HTMLDetailsElement)) return;
  if (details.open) openedBuildSources.value.add(ref);
  else openedBuildSources.value.delete(ref);
}
const confirmationAction = ref<"ARCHIVE" | "RESTORE">();
const recipe = computed(() =>
  props.recipeRef ? store.recipes[props.recipeRef] : undefined,
);
const sourceVisible = computed(() =>
  recipe.value
    ? recipe.value.sourceAvailable &&
      typeof recipe.value.environment.dockerfile === "string"
    : !props.recipeRef && store.createAllowed[props.projectRef] === true,
);
const builds = computed(() =>
  props.recipeRef ? (store.builds[props.recipeRef] ?? []) : [],
);
const currentBuild = computed(() => latestBuild(builds.value));
const artifact = computed(() =>
  props.recipeRef ? store.artifacts[props.recipeRef] : undefined,
);
const revisions = computed(() =>
  props.recipeRef ? (store.revisions[props.recipeRef] ?? []) : [],
);
const promotionReceipt = computed(() =>
  props.recipeRef ? store.promotionReceipts[props.recipeRef] : undefined,
);
const buildActive = computed(() =>
  currentBuild.value ? buildIsActive(currentBuild.value) : false,
);
const promotionPending = computed(
  () =>
    !recipe.value?.promotedImageReady &&
    ["QUEUED", "PROMOTING"].includes(promotionReceipt.value?.state ?? ""),
);
const promotionVisualState = computed(() => {
  if (recipe.value?.promotedImageReady) return "PROMOTED";
  if (promotionReceipt.value?.state === "PROMOTING") return "RUNNING";
  return promotionReceipt.value?.state ?? "PENDING";
});
const dependencies = computed(() =>
  props.recipeRef ? (store.dependencies[props.recipeRef] ?? []) : [],
);
const dockerfileMessages = computed(() =>
  validateDockerfile(dockerfile.value).map((key) => t(key)),
);
const selectedEnvironment = computed(() =>
  store.environments.find((item) => item.key === environmentKey.value),
);
const hasLocalChanges = computed(() =>
  Boolean(
    recipe.value &&
    sourceVisible.value &&
    (name.value !== recipe.value.name ||
      environmentKey.value !== recipe.value.environment.environmentKey ||
      dockerfile.value !== recipe.value.environment.dockerfile),
  ),
);
const canSave = computed(
  () =>
    sourceVisible.value &&
    name.value.trim().length > 0 &&
    Boolean(roleDefinitionRef.value) &&
    Boolean(selectedEnvironment.value?.available) &&
    dockerfileMessages.value.length === 0 &&
    (!recipe.value || recipe.value.nextActions.includes("UPDATE")),
);
const roleLabel = computed(
  () =>
    store.roleDefinitionByRef.get(
      recipe.value?.roleDefinitionRef ?? roleDefinitionRef.value,
    )?.label ?? t("roleImages.unknownRole"),
);
const environmentLabel = computed(() => {
  const key = environmentKey.value;
  if (!key) return t("common.noData");
  const environment = store.environmentByKey.get(key);
  return environment ? t(environment.nameMessageKey) : key;
});
let buildPollTimer: ReturnType<typeof setTimeout> | undefined;
let lifecyclePollAttempts = 0;
let disposed = false;
let loadGeneration = 0;

function stopBuildPolling(): void {
  if (buildPollTimer) clearTimeout(buildPollTimer);
  buildPollTimer = undefined;
}

function scheduleBuildPolling(): void {
  stopBuildPolling();
  if (
    disposed ||
    !props.recipeRef ||
    (!buildActive.value && !promotionPending.value) ||
    lifecyclePollAttempts >= 150
  )
    return;
  buildPollTimer = setTimeout(() => void refreshBuild(), 2000);
}

async function refreshBuild(): Promise<void> {
  if (disposed || !props.recipeRef) return;
  const current = loadGeneration;
  lifecyclePollAttempts += 1;
  await store.loadDetail(props.projectRef, props.recipeRef, false);
  if (current === loadGeneration) scheduleBuildPolling();
}

function sync(): void {
  if (!recipe.value) return;
  name.value = recipe.value.name;
  roleDefinitionRef.value = recipe.value.roleDefinitionRef;
  environmentKey.value = recipe.value.environment.environmentKey;
  dockerfile.value = sourceVisible.value
    ? (recipe.value.environment.dockerfile ?? "")
    : "";
}

async function load(): Promise<void> {
  const current = ++loadGeneration;
  lifecyclePollAttempts = 0;
  const tasks: Promise<void>[] = [
    store.loadSupportingCatalogs(props.projectRef),
  ];
  if (props.recipeRef)
    tasks.push(store.loadDetail(props.projectRef, props.recipeRef));
  await Promise.all(tasks);
  if (disposed || current !== loadGeneration) return;
  if (!props.recipeRef && !environmentKey.value) {
    const recommended = store.environments.find(
      (environment) => environment.available && environment.recommended,
    );
    if (recommended) selectEnvironment(recommended.key);
  }
  sync();
  scheduleBuildPolling();
}

function selectEnvironment(key: string): void {
  if (!sourceVisible.value) return;
  environmentKey.value = key;
  const environment = store.environments.find((item) => item.key === key);
  dockerfile.value = environment?.dockerfileTemplate ?? "";
}

async function save(): Promise<void> {
  if (!canSave.value || !selectedEnvironment.value) return;
  const keepsEnvironmentSelection =
    recipe.value?.environment.environmentKey === selectedEnvironment.value.key;
  const selection = {
    environmentKey: selectedEnvironment.value.key,
    dockerfile: dockerfile.value.replace(/\r\n?/g, "\n"),
    ...(keepsEnvironmentSelection && recipe.value.environment.packageKeys
      ? { packageKeys: [...recipe.value.environment.packageKeys] }
      : {}),
    ...(keepsEnvironmentSelection && recipe.value.environment.toolKeys
      ? { toolKeys: [...recipe.value.environment.toolKeys] }
      : {}),
    ...(keepsEnvironmentSelection && recipe.value.environment.installationBlock
      ? { installationBlock: recipe.value.environment.installationBlock }
      : {}),
  };
  try {
    if (recipe.value) {
      await store.update(props.projectRef, recipe.value, {
        name: name.value.trim(),
        environment: selection,
      });
      sync();
      return;
    }
    const created = await store.create(props.projectRef, {
      roleDefinitionRef: roleDefinitionRef.value,
      name: name.value.trim(),
      environment: selection,
    });
    await router.replace(
      `/projects/${encodeURIComponent(props.projectRef)}/role-images/${encodeURIComponent(created.ref)}`,
    );
  } catch {
    // Store сохраняет нормализованную problem-модель для видимого состояния.
  }
}

async function runCommand(
  action: "REQUEST_BUILD" | "ARCHIVE" | "RESTORE",
): Promise<void> {
  if (!recipe.value || store.mutating || hasLocalChanges.value) return;
  try {
    await store.command(props.projectRef, recipe.value, action);
    lifecyclePollAttempts = 0;
    confirmationAction.value = undefined;
    sync();
    scheduleBuildPolling();
  } catch {
    // Store сохраняет нормализованную problem-модель для видимого состояния.
  }
}

async function confirmLifecycle(): Promise<void> {
  if (!confirmationAction.value) return;
  await runCommand(confirmationAction.value);
}

async function promote(): Promise<void> {
  if (
    !recipe.value ||
    !artifact.value ||
    store.mutating ||
    hasLocalChanges.value
  )
    return;
  if (!canPromoteRoleImage(recipe.value, artifact.value)) return;
  try {
    await store.promote(props.projectRef, recipe.value, artifact.value);
    lifecyclePollAttempts = 0;
    scheduleBuildPolling();
    sync();
  } catch {
    // Store сохраняет нормализованную problem-модель для видимого состояния.
  }
}

watch(
  sourceVisible,
  (value) => {
    if (value) return;
    dockerfile.value = "";
    diffOpen.value = false;
    openedBuildSources.value.clear();
  },
  { flush: "sync" },
);
watch(
  () => [props.projectRef, props.recipeRef],
  () => {
    stopBuildPolling();
    void load();
  },
);
onMounted(() => void load());
onBeforeUnmount(() => {
  disposed = true;
  loadGeneration += 1;
  stopBuildPolling();
  store.dispose();
});
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
    <template v-else-if="!props.recipeRef || recipe">
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
            :disabled="store.mutating || hasLocalChanges"
            @click="runCommand('REQUEST_BUILD')"
          >
            <Hammer :size="16" aria-hidden="true" />
            {{ t("roleImages.requestBuild") }}
          </button>
          <button
            v-if="recipe.nextActions.includes('ARCHIVE')"
            class="button"
            type="button"
            :disabled="store.mutating || hasLocalChanges"
            @click="confirmationAction = 'ARCHIVE'"
          >
            <Archive :size="16" aria-hidden="true" />
            {{ t("common.archive") }}
          </button>
          <button
            v-if="recipe.nextActions.includes('RESTORE')"
            class="button"
            type="button"
            :disabled="store.mutating || hasLocalChanges"
            @click="confirmationAction = 'RESTORE'"
          >
            <RotateCcw :size="16" aria-hidden="true" />
            {{ t("roleImages.restore") }}
          </button>
          <button
            v-if="canPromoteRoleImage(recipe, artifact)"
            class="button button--primary"
            type="button"
            :disabled="store.mutating || hasLocalChanges"
            @click="promote"
          >
            <PackageCheck :size="16" aria-hidden="true" />
            {{ t("roleImages.promotion") }}
          </button>
        </div>
      </section>

      <RoleImageLineage
        v-if="recipe"
        class="panel"
        :lineage="recipe.managedLineage"
      />
      <section v-if="recipe" class="image-lifecycle" aria-live="polite">
        <article class="panel lifecycle-step">
          <Hammer :size="18" aria-hidden="true" />
          <div>
            <span>{{ t("roleImages.buildHistory") }}</span>
            <strong>
              {{
                currentBuild
                  ? t(`states.${currentBuild.stage}`)
                  : t("states.PENDING")
              }}
            </strong>
            <small v-if="currentBuild">
              {{ currentBuild.progressPercent }}% ·
              {{ new Date(currentBuild.updatedAt).toLocaleString() }}
            </small>
          </div>
          <StatusBadge :state="currentBuild?.stage ?? 'PENDING'" />
        </article>
        <article class="panel lifecycle-step">
          <ShieldCheck :size="18" aria-hidden="true" />
          <div>
            <span>{{ t("roleImages.admissionVerdict") }}</span>
            <strong>
              {{ artifact ? t("roleImages.evidence") : t("states.PENDING") }}
            </strong>
            <small v-if="artifact">{{ artifact.manifestDigest }}</small>
          </div>
          <StatusBadge
            :state="artifact?.admissionVerdict ?? 'PENDING'"
            :label="
              artifact?.admissionVerdict === 'ACCEPTED'
                ? t('states.APPROVED')
                : artifact?.admissionVerdict === 'REJECTED'
                  ? t('states.REJECTED')
                  : t('states.PENDING')
            "
          />
        </article>
        <article class="panel lifecycle-step">
          <PackageCheck :size="18" aria-hidden="true" />
          <div>
            <span>{{ t("roleImages.promotion") }}</span>
            <strong>{{ t(`states.${promotionVisualState}`) }}</strong>
            <small v-if="recipe.promotedImageReference">
              {{ recipe.promotedImageReference }}
            </small>
          </div>
          <StatusBadge :state="promotionVisualState" />
        </article>
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
                :state="recipe ? 'ACTIVE' : 'DRAFT'"
                :label="
                  recipe
                    ? t('roleImages.generationLabel', {
                        generation: recipe.generation,
                      })
                    : t('roleImages.localDraft')
                "
              />
            </header>
            <div class="recipe-fields">
              <label class="field">
                <span>{{ t("common.name") }}</span>
                <input
                  v-model="name"
                  maxlength="120"
                  :readonly="!!recipe && !recipe.nextActions.includes('UPDATE')"
                />
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
              <label class="field">
                <span>{{ t("roleImages.environment") }}</span>
                <select
                  :value="environmentKey"
                  :disabled="
                    !store.environments.length ||
                    (!!recipe && !recipe.nextActions.includes('UPDATE'))
                  "
                  @change="
                    selectEnvironment(
                      ($event.currentTarget as HTMLSelectElement).value,
                    )
                  "
                >
                  <option value="" disabled>
                    {{ t("roleImages.chooseEnvironment") }}
                  </option>
                  <option
                    v-for="environment in store.environments"
                    :key="environment.key"
                    :value="environment.key"
                    :disabled="!environment.available"
                  >
                    {{ t(environment.nameMessageKey) }}
                    {{
                      environment.recommended
                        ? `· ${t("roleImages.recommended")}`
                        : ""
                    }}
                  </option>
                </select>
              </label>
            </div>
            <RoleImageDockerfileEditor
              v-if="sourceVisible"
              v-model="dockerfile"
              :label="t('roleImages.dockerfile')"
              :validation-messages="dockerfileMessages"
              :readonly="
                store.mutating ||
                (!!recipe && !recipe.nextActions.includes('UPDATE'))
              "
            />
            <p v-else class="source-unavailable">
              {{ t("roleImages.sourceUnavailable") }}
            </p>
            <button
              v-if="
                sourceVisible &&
                recipe &&
                dockerfile !== recipe.environment.dockerfile
              "
              type="button"
              class="button"
              :aria-expanded="diffOpen"
              @click="diffOpen = !diffOpen"
            >
              {{ t("managed.diff") }}
            </button>
            <CodeDiff
              v-if="sourceVisible && diffOpen && recipe"
              :original="recipe.environment.dockerfile ?? ''"
              :modified="dockerfile"
              :label="t('managed.diff')"
            />
            <div class="save-boundary">
              <p>
                {{
                  recipe
                    ? t("roleImages.immutableRevisionHelp")
                    : t("roleImages.createHelp")
                }}
              </p>
              <button
                class="button button--primary"
                type="button"
                :disabled="store.mutating || !canSave"
                @click="save"
              >
                {{
                  recipe ? t("roleImages.createRevision") : t("common.create")
                }}
              </button>
            </div>
          </section>

          <component
            v-if="recipe"
            :is="buildsExpanded ? ModalDialog : 'section'"
            class="build-history"
            :class="{ 'build-history--expanded': buildsExpanded }"
            :title="buildsExpanded ? t('roleImages.buildHistory') : undefined"
            size="full"
            @close="buildsExpanded = false"
          >
            <header class="section-header">
              <div>
                <h2>{{ t("roleImages.buildHistory") }}</h2>
                <p>{{ t("roleImages.buildHistoryHelp") }}</p>
              </div>
              <button
                v-if="!buildsExpanded"
                class="icon-button"
                type="button"
                :title="t('catalog.expand')"
                :aria-label="t('catalog.expand')"
                @click="buildsExpanded = true"
              >
                <Maximize2 :size="20" />
              </button>
            </header>
            <div class="build-history__scroll">
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
                  <code>
                    {{
                      t("roleImages.generationLabel", {
                        generation: buildRevisionIdentity(build).generation,
                      })
                    }}
                  </code>
                  <small>{{
                    new Date(build.updatedAt).toLocaleString()
                  }}</small>
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
                <details
                  v-if="
                    build.sourceAvailable &&
                    typeof build.dockerfile === 'string'
                  "
                  class="build-source"
                  @toggle="toggleBuildSource(build.ref, $event)"
                >
                  <summary>
                    {{ t("roleImages.dockerfile") }} ·
                    {{
                      t("roleImages.generationLabel", {
                        generation: build.recipeGeneration,
                      })
                    }}
                  </summary>
                  <p v-if="build.configurationRevisionRef">
                    {{ t("roleImages.configuration") }}:
                    {{ build.configurationRevisionRef }}
                  </p>
                  <CodeEditor
                    v-if="openedBuildSources.has(build.ref)"
                    :model-value="build.dockerfile"
                    :label="t('roleImages.dockerfile')"
                    language="dockerfile"
                    readonly
                  />
                </details>
              </article>
            </div>
          </component>

          <component
            v-if="recipe"
            :is="revisionsExpanded ? ModalDialog : 'section'"
            class="build-history"
            :class="{ 'build-history--expanded': revisionsExpanded }"
            :title="
              revisionsExpanded ? t('runtime.revisionHistory') : undefined
            "
            size="full"
            @close="revisionsExpanded = false"
          >
            <header class="section-header">
              <div>
                <h2>{{ t("runtime.revisionHistory") }}</h2>
                <p>{{ t("roleImages.immutableRevisionHelp") }}</p>
              </div>
              <button
                v-if="!revisionsExpanded"
                class="icon-button"
                type="button"
                :title="t('catalog.expand')"
                :aria-label="t('catalog.expand')"
                @click="revisionsExpanded = true"
              >
                <Maximize2 :size="20" />
              </button>
            </header>
            <div class="build-history__scroll build-history__scroll--revisions">
              <div v-if="!revisions.length" class="empty-section">
                {{ t("common.empty") }}
              </div>
              <article
                v-for="revision in revisions"
                v-else
                :key="revision.ref"
                class="revision-row"
              >
                <div>
                  <strong>rev {{ revision.revision }}</strong>
                  <span>
                    {{
                      t("roleImages.generationLabel", {
                        generation: revision.recipeGeneration,
                      })
                    }}
                  </span>
                </div>
                <code :title="revision.manifestDigest">{{
                  revision.manifestDigest
                }}</code>
                <StatusBadge
                  :state="revision.promotedReference ? 'PROMOTED' : 'COMPLETED'"
                />
                <small>{{
                  new Date(revision.createdAt).toLocaleString()
                }}</small>
              </article>
              <button
                v-if="store.revisionNextPageToken[recipe.ref]"
                class="button"
                type="button"
                :disabled="store.loadingDetail"
                @click="store.loadMoreRevisions(projectRef, recipe.ref)"
              >
                {{ t("roleImages.loadMore") }}
              </button>
            </div>
          </component>
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
              <div>
                <dt>{{ t("runtime.revisionHistory") }}</dt>
                <dd>{{ revisions.length }}</dd>
              </div>
            </dl>
          </section>

          <section class="panel artifact-card">
            <ShieldCheck :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.evidence") }}</h2>
            <dl v-if="artifact">
              <div>
                <dt>{{ t("roleImages.manifestDigest") }}</dt>
                <dd>
                  <code>{{ artifact.manifestDigest }}</code>
                </dd>
              </div>
              <div>
                <dt>SBOM SHA-256</dt>
                <dd>
                  <code>{{ artifact.sbomSha256 ?? "—" }}</code>
                </dd>
              </div>
              <div>
                <dt>{{ t("roleImages.vulnerabilityEvidence") }}</dt>
                <dd>
                  <code>{{ artifact.vulnerabilityEvidenceSha256 ?? "—" }}</code>
                </dd>
              </div>
              <div>
                <dt>{{ t("roleImages.admissionVerdict") }}</dt>
                <dd><StatusBadge :state="artifact.admissionVerdict" /></dd>
              </div>
              <div>
                <dt>Provenance</dt>
                <dd>
                  <code>{{ artifact.provenanceSha256 }}</code>
                </dd>
              </div>
              <div v-if="artifact.promotionReceiptSha256">
                <dt>{{ t("roleImages.promotion") }}</dt>
                <dd>
                  <code>{{ artifact.promotionReceiptSha256 }}</code>
                </dd>
              </div>
            </dl>
            <StatusBadge
              v-if="promotionReceipt"
              :state="promotionReceipt.state"
            />
            <p v-else>{{ t("roleImages.noPromotedArtifact") }}</p>
          </section>
          <section class="panel artifact-card">
            <TerminalSquare :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.executables") }}</h2>
            <ul v-if="artifact?.tools.length" class="tool-list">
              <li v-for="tool in artifact.tools" :key="tool.name">
                <code>{{ tool.name }}</code
                ><span>{{ tool.version }}</span>
              </li>
            </ul>
            <p v-else>{{ t("roleImages.noVerifiedExecutables") }}</p>
          </section>
          <section class="panel artifact-card">
            <Link2 :size="20" aria-hidden="true" />
            <h2>{{ t("roleImages.usedByEnvironments") }}</h2>
            <ul v-if="dependencies.length" class="dependency-list">
              <li v-for="environment in dependencies" :key="environment.ref">
                <RouterLink
                  :to="`/projects/${encodeURIComponent(projectRef)}/environments/${encodeURIComponent(environment.ref)}`"
                >
                  {{ environment.name }}
                </RouterLink>
                <span>rev {{ environment.currentVersion.revision }}</span>
              </li>
            </ul>
            <p v-else>{{ t("roleImages.noEnvironmentDependencies") }}</p>
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
    <ModalDialog
      v-if="confirmationAction && recipe"
      :title="
        t(
          confirmationAction === 'ARCHIVE'
            ? 'roleImages.confirmArchive'
            : 'roleImages.confirmRestore',
        )
      "
      :busy="store.mutating"
      size="md"
      @close="confirmationAction = undefined"
    >
      <div class="lifecycle-confirmation">
        <Box :size="24" aria-hidden="true" />
        <div>
          <strong>{{ recipe.name }}</strong>
          <p>{{ roleLabel }}</p>
        </div>
      </div>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="store.mutating"
          @click="confirmationAction = undefined"
        >
          {{ t("common.cancel") }}
        </button>
        <button
          class="button"
          :class="{ 'button--danger': confirmationAction === 'ARCHIVE' }"
          type="button"
          :disabled="store.mutating"
          @click="confirmLifecycle"
        >
          <Archive
            v-if="confirmationAction === 'ARCHIVE'"
            :size="16"
            aria-hidden="true"
          />
          <RotateCcw v-else :size="16" aria-hidden="true" />
          {{
            confirmationAction === "ARCHIVE"
              ? t("common.archive")
              : t("roleImages.restore")
          }}
        </button>
      </template>
    </ModalDialog>
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
.lifecycle-confirmation {
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}
.lifecycle-confirmation p {
  margin: 5px 0 0;
  color: var(--text-secondary);
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
.image-lifecycle {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.lifecycle-step {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
}
.lifecycle-step > svg {
  margin-top: 2px;
  color: var(--accent-strong);
}
.lifecycle-step > div {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.lifecycle-step span,
.lifecycle-step small {
  overflow: hidden;
  color: var(--text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
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
  grid-template-columns: repeat(3, minmax(0, 1fr));
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
  gap: 3px;
}
.build-row > div:first-child code {
  color: var(--text-secondary);
  font-size: 0.75rem;
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
.build-source {
  grid-column: 1 / -1;
}
.build-source summary {
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 0.8rem;
}
.build-source pre {
  max-height: 280px;
  padding: 12px;
  margin: 10px 0 0;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--canvas);
}
.build-source code {
  font-family: var(--font-mono);
  font-size: 0.76rem;
  white-space: pre;
}
.revision-row {
  min-height: 88px;
  display: grid;
  grid-template-columns: minmax(160px, 0.5fr) minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-top: 1px solid var(--hairline);
}
.build-history__scroll {
  max-height: 768px;
  overflow: auto;
}
.build-history__scroll .build-row {
  min-height: 128px;
}
.build-history__scroll--revisions {
  max-height: 528px;
}
.build-history--expanded .build-history__scroll {
  max-height: calc(100dvh - 210px);
}
.revision-row > div {
  display: grid;
  gap: 3px;
}
.revision-row code {
  overflow: hidden;
  font-family: var(--font-mono);
  font-size: 0.76rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.revision-row small,
.revision-row span {
  color: var(--text-secondary);
}
.empty-section {
  padding: 28px;
  border-top: 1px solid var(--border);
  color: var(--text-secondary);
  text-align: center;
}
.facts-panel h2,
.artifact-card h2 {
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
.artifact-card {
  display: grid;
  gap: 10px;
}
.artifact-card > svg {
  color: var(--accent-strong);
}
.artifact-card dl,
.tool-list,
.dependency-list {
  display: grid;
  gap: 9px;
  padding: 0;
  margin: 0;
  list-style: none;
}
.artifact-card dl > div,
.tool-list li,
.dependency-list li {
  display: grid;
  gap: 3px;
  padding-bottom: 9px;
  border-bottom: 1px solid var(--hairline);
}
.artifact-card dt,
.tool-list span,
.dependency-list span,
.artifact-card > p {
  color: var(--text-secondary);
  font-size: 0.8rem;
}
.artifact-card dd,
.artifact-card > p {
  margin: 0;
}
.artifact-card code {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 1000px) {
  .image-lifecycle {
    grid-template-columns: 1fr;
  }
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
