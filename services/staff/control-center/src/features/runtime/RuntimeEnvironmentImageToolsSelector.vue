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
import RuntimeEnvironmentToolsEditor from "./RuntimeEnvironmentToolsEditor.vue";
import {
  assertPromotedRuntimeImage,
  runtimeImageOption,
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
const artifact = ref<RoleImageArtifact>();
const selected = ref<RuntimeImageOption>();
const loading = ref(false);
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
  option: RuntimeImageOption,
  choosing: boolean,
): Promise<void> {
  invalidate();
  const current = generation;
  controller = new AbortController();
  const signal = controller.signal;
  loading.value = true;
  try {
    const result = await props.catalog.loadArtifact(
      props.resourceScope,
      option.recipeRef,
      option.ref,
      signal,
    );
    if (signal.aborted || current !== generation) return;
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
    if (!image || image.artifactRef !== props.imageArtifactRef) return;
    const option: RuntimeImageOption = {
      ref: image.artifactRef,
      recipeRef: image.recipeRef,
      generation: image.recipeGeneration,
      title: t("runtime.exactImage"),
      description: image.reference,
    };
    selected.value = option;
    void load(option, false);
  },
  { immediate: true },
);
onBeforeUnmount(invalidate);

function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
) {
  return props.catalog.loadPage(
    props.resourceScope,
    query,
    cursor,
    signal,
    pageSize,
  );
}
</script>

<template>
  <section class="image-tools-selector">
    <div class="field">
      <span>{{ $t("runtime.exactImage") }}</span>
      <AsyncEntityPicker
        :model-value="imageArtifactRef"
        :selected="selected"
        :context-key="scopeKey"
        :load-page="loadPage"
        :placeholder="$t('runtime.choosePromotedImage')"
        :search-placeholder="$t('runtime.searchPromotedImage')"
        :disabled="disabled || loading"
        @update:model-value="clear"
        @select="select"
      />
    </div>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <RuntimeEnvironmentToolsEditor
      :tools="tools"
      :catalog="artifact?.tools ?? []"
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
</style>
