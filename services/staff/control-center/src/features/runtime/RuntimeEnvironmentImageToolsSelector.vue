<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  RoleImageArtifact,
  RuntimeEnvironmentImage,
  RuntimeEnvironmentTool,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import RuntimeEnvironmentToolsEditor from "./RuntimeEnvironmentToolsEditor.vue";
import {
  verifiedImageInventoryAvailable,
  verifiedImageTools,
} from "@/shared/lib/verified-image-tools";
import {
  assertPromotedRuntimeImage,
  runtimeImageOption,
  runtimeImagePagePresentation,
  restoreRuntimeImageOption,
  toolsForRuntimeImage,
  type RuntimeImageCatalog,
  type RuntimeImageOption,
} from "./image-tools-selection";
import {
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
} from "./resource-scope";

const props = defineProps<{
  resourceScope: RuntimeResourceScope;
  imageArtifactRef: string;
  currentImage?: RuntimeEnvironmentImage;
  tools: RuntimeEnvironmentTool[];
  catalog: RuntimeImageCatalog;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "update:imageArtifactRef": [value: string];
  "update:tools": [value: RuntimeEnvironmentTool[]];
  "availability-change": [ready: boolean];
}>();
const { t } = useI18n();
const localizeServerMessage = useServerMessage();
const artifact = ref<RoleImageArtifact>();
const selected = ref<RuntimeImageOption>();
const pinnedImage = computed(() =>
  props.currentImage?.artifactRef === props.imageArtifactRef
    ? props.currentImage
    : undefined,
);
const localizedSelected = computed(() =>
  selected.value
    ? { ...selected.value, title: localizeServerMessage(selected.value.title) }
    : undefined,
);
const loading = ref(false);
const pickerPlaceholder = computed(() =>
  t(
    loading.value && props.imageArtifactRef
      ? "runtime.loadingSelectedImage"
      : "runtime.choosePromotedImage",
  ),
);
const pickerTriggerLabel = computed(() =>
  localizedSelected.value?.ref === props.imageArtifactRef
    ? localizedSelected.value.title
    : pickerPlaceholder.value,
);
const problem = ref<AppProblem>();
const scopeKey = computed(() => runtimeResourceScopeKey(props.resourceScope));
let generation = 0;
let controller: AbortController | undefined;

function invalidate(): void {
  generation += 1;
  controller?.abort();
  artifact.value = undefined;
  problem.value = undefined;
  loading.value = false;
  emit("availability-change", false);
}

async function load(
  option: RuntimeImageOption | undefined,
  choosing: boolean,
): Promise<void> {
  invalidate();
  const current = generation;
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    option ??= await restoreRuntimeImageOption(
      props.catalog,
      props.resourceScope,
      props.imageArtifactRef,
      signal,
    );
    const obsolete = () => signal.aborted || current !== generation;
    if (obsolete()) return;
    const result = await props.catalog.loadArtifact(
      props.resourceScope,
      option.recipeRef,
      option.ref,
      signal,
    );
    if (obsolete()) return;
    assertPromotedRuntimeImage(result.artifact, {
      artifactRef: option.ref,
      recipeRef: option.recipeRef,
      recipeGeneration: option.generation,
    });
    artifact.value = result.artifact;
    selected.value = {
      ...option,
      title: result.recipeName,
      description: result.artifact.promotedReference,
    };
    if (choosing) {
      emit("update:tools", toolsForRuntimeImage(props.tools, result.artifact));
      emit("update:imageArtifactRef", result.artifact.ref);
    }
    emit("availability-change", true);
  } catch (error) {
    if (signal.aborted || current !== generation) return;
    problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}

async function select(option: AsyncEntityOption): Promise<void> {
  if (props.disabled) return;
  try {
    await load(runtimeImageOption(option), true);
  } catch (error) {
    invalidate();
    problem.value = asProblem(error);
  }
}

function clear(value: unknown): void {
  if (props.disabled || value) return;
  invalidate();
  selected.value = undefined;
  emit("update:imageArtifactRef", "");
  emit("update:tools", []);
}

watch(
  [scopeKey, () => props.imageArtifactRef, () => props.currentImage],
  (next, previous) => {
    if (
      next[0] === previous[0] &&
      artifact.value?.ref === props.imageArtifactRef
    )
      return;
    const image = props.currentImage;
    invalidate();
    selected.value = undefined;
    if (!props.imageArtifactRef) return;
    if (!image || image.artifactRef !== props.imageArtifactRef) {
      void load(undefined, false);
      return;
    }
    const option: RuntimeImageOption = {
      ref: image.artifactRef,
      recipeRef: image.recipeRef,
      generation: image.recipeGeneration,
      title: `${t("runtime.exactImage")} · ${t("roleImages.generationLabel", { generation: image.recipeGeneration })}`,
      description: image.reference,
    };
    selected.value = option;
    void load(option, false);
  },
  { immediate: true },
);
onBeforeUnmount(invalidate);

async function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
) {
  const page = await props.catalog.loadPage(
    props.resourceScope,
    query,
    cursor,
    signal,
    pageSize,
  );
  return {
    ...page,
    items: runtimeImagePagePresentation(
      page.items.map((option) => ({
        ...option,
        title: localizeServerMessage(option.title),
      })),
      (generation) => t("roleImages.generationLabel", { generation }),
    ),
  };
}
</script>

<template>
  <section class="image-tools-selector">
    <div class="field">
      <span>{{ $t("runtime.exactImage") }}</span>
      <AsyncEntityPicker
        :model-value="imageArtifactRef"
        :selected="localizedSelected"
        :context-key="scopeKey"
        :load-page="loadPage"
        :placeholder="pickerPlaceholder"
        :trigger-label="pickerTriggerLabel"
        :search-placeholder="$t('runtime.searchPromotedImage')"
        :disabled="disabled || loading"
        @update:model-value="clear"
        @select="select"
      />
    </div>
    <div
      v-if="pinnedImage && !artifact && !loading"
      class="image-tools-selector__pinned"
    >
      <span>{{ $t("runtime.exactImage") }}</span>
      <code>{{ pinnedImage.reference }}</code>
      <small>{{ pinnedImage.digest }}</small>
    </div>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <RuntimeEnvironmentToolsEditor
      :tools="tools"
      :catalog="verifiedImageTools(artifact)"
      :inventory-available="verifiedImageInventoryAvailable(artifact)"
      :image-selected="Boolean(imageArtifactRef)"
      :loading="loading"
      :disabled="disabled || loading || !artifact || !!problem"
      @update:tools="emit('update:tools', $event)"
    />
  </section>
</template>

<style scoped>
.image-tools-selector {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.field {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.image-tools-selector__pinned {
  display: grid;
  gap: 4px;
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--muted);
}
</style>
