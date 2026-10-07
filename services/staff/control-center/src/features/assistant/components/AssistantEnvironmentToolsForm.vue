<script setup lang="ts">
import { computed, ref, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import { useRuntimeStore } from "@/features/runtime/store";
import {
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
} from "@/features/runtime/resource-scope";
import type {
  RuntimeImageCatalog,
  RuntimeImageOption,
} from "@/features/runtime/image-tools-selection";
import RuntimeEnvironmentToolsEditor from "@/features/runtime/RuntimeEnvironmentToolsEditor.vue";
import {
  verifiedImageInventoryAvailable,
  verifiedImageTools,
} from "@/shared/lib/verified-image-tools";
import {
  defaultRuntimeEnvironmentPolicy,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import type {
  RoleImageArtifact,
  RuntimeEnvironmentTool,
} from "@/shared/api/generated/openapi/types.gen";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef: string;
  selectedImage?: AsyncEntityOption;
  disabled: boolean;
  resourceScope?: RuntimeResourceScope;
  imageCatalog?: RuntimeImageCatalog;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
  "resolved-image": [value: RuntimeImageOption];
}>();
const runtime = useRuntimeStore();
const artifact = ref<RoleImageArtifact>();
const loading = ref(false);
const loadFailed = ref(false);

function object(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function parameter(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}

const imageRef = computed(() => {
  const value = parameter("imageArtifactRef");
  return typeof value === "string" ? value : "";
});

function decodeTools(raw: unknown): RuntimeEnvironmentTool[] | undefined {
  if (raw === undefined || raw === null) return [];
  if (!Array.isArray(raw) || raw.length > 128) return undefined;
  const result: RuntimeEnvironmentTool[] = [];
  for (const entry of raw) {
    const item = object(entry);
    if (
      !item ||
      Object.keys(item).some(
        (key) =>
          ![
            "name",
            "command",
            "description",
            "usageHint",
            "Name",
            "Command",
            "Description",
            "UsageHint",
          ].includes(key),
      )
    )
      return undefined;
    const name = item.name ?? item.Name;
    const command = item.command ?? item.Command;
    const description = item.description ?? item.Description;
    const usageHint = item.usageHint ?? item.UsageHint ?? "";
    if (
      [name, command, description, usageHint].some(
        (value) => typeof value !== "string",
      )
    )
      return undefined;
    result.push({
      name: name as string,
      command: command as string,
      description: description as string,
      usageHint: usageHint as string,
    });
  }
  return result;
}

const suppliedTools = computed(() => parameter("tools"));
const tools = computed(() =>
  decodeTools(
    suppliedTools.value === undefined
      ? object(props.operation.value.before.specification)?.Tools
      : suppliedTools.value,
  ),
);

const problems = computed(() => {
  if (!tools.value) return ["runtime.errors.toolCommand"];
  const result = validateEnvironmentInput({
    name: "Assistant draft",
    description: "",
    imageArtifactRef: "imgart_example",
    tools: tools.value,
    values: [],
    secretBindings: [],
    policy: defaultRuntimeEnvironmentPolicy(),
  })
    .filter((problem) => problem.field.startsWith("tools"))
    .map((problem) => problem.message);
  if (
    suppliedTools.value !== undefined &&
    tools.value.length > 0 &&
    (!verifiedImageInventoryAvailable(artifact.value) ||
      tools.value.some(
        (tool) =>
          !verifiedImageTools(artifact.value).some(
            (available) => available.name === tool.command,
          ),
      ))
  )
    result.push("assistant.planEditor.environmentToolsUnverified");
  return [...new Set(result)];
});
const visibleProblems = computed(() =>
  problems.value.filter(
    (problem) =>
      !loading.value ||
      problem !== "assistant.planEditor.environmentToolsUnverified",
  ),
);
watch(
  () => problems.value.length === 0,
  (valid) => emit("valid", valid),
  { immediate: true },
);

watch(
  [
    () => props.projectRef,
    () =>
      props.resourceScope ? runtimeResourceScopeKey(props.resourceScope) : "",
    () => imageRef.value,
    () => props.selectedImage?.ref,
    () =>
      props.selectedImage && "recipeRef" in props.selectedImage
        ? props.selectedImage.recipeRef
        : "",
    () => props.imageCatalog,
  ] as const,
  async ([projectRef, , ref, , chosenRecipe, catalog], _, onCleanup) => {
    const scope = props.resourceScope;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    artifact.value = undefined;
    loadFailed.value = false;
    loading.value = false;
    if ((!projectRef && !scope) || !ref) return;
    loading.value = true;
    try {
      if (scope?.kind === "ORGANIZATION" && !catalog)
        throw new Error("Organization image catalog is unavailable");
      let recipeRef = typeof chosenRecipe === "string" ? chosenRecipe : "";
      if (!recipeRef) {
        let cursor: string | undefined;
        const visited = new Set<string>();
        do {
          const page =
            scope && catalog
              ? await catalog.loadPage(scope, "", cursor, controller.signal)
              : await runtime.searchPromotedRoleImagePage(
                  projectRef,
                  "",
                  cursor,
                  controller.signal,
                );
          const match = page.items.find((item) => item.ref === ref);
          if (
            match &&
            "recipeRef" in match &&
            typeof match.recipeRef === "string"
          ) {
            recipeRef = match.recipeRef;
            break;
          }
          cursor = page.nextPageToken;
          if (cursor && visited.has(cursor))
            throw new Error(
              "Role image catalog returned a repeated page token",
            );
          if (cursor) visited.add(cursor);
        } while (cursor && visited.size < 100);
      }
      if (!recipeRef)
        throw new Error(
          "Promoted image is not available in the project catalog",
        );
      const loaded =
        scope && catalog
          ? await catalog.loadArtifact(scope, recipeRef, ref, controller.signal)
          : await runtime.loadPromotedRoleImageArtifact(
              projectRef,
              recipeRef,
              ref,
              controller.signal,
            );
      if (!controller.signal.aborted) {
        artifact.value = loaded.artifact;
        emit("resolved-image", {
          ref,
          title: loaded.recipeName,
          description: loaded.artifact.promotedReference,
          recipeRef,
          generation: loaded.artifact.recipeGeneration,
        });
      }
    } catch {
      if (!controller.signal.aborted) loadFailed.value = true;
    } finally {
      if (!controller.signal.aborted) loading.value = false;
    }
  },
  { immediate: true },
);

function update(next: RuntimeEnvironmentTool[]): void {
  emit("parameter", "tools", next);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-environment-tools">
    <RuntimeEnvironmentToolsEditor
      :tools="tools ?? []"
      :catalog="verifiedImageTools(artifact)"
      :inventory-available="verifiedImageInventoryAvailable(artifact)"
      :image-selected="!!imageRef"
      :loading="loading"
      :disabled="disabled || !tools || !artifact"
      @update:tools="update"
    />
    <p v-if="!loading && !artifact && tools?.length" class="secondary-text">
      {{ $t("assistant.planEditor.environmentToolsPending") }}
      <code v-for="tool in tools" :key="tool.command">{{ tool.command }}</code>
    </p>
    <p v-if="loadFailed && imageRef" class="field-error" role="alert">
      {{ $t("assistant.planEditor.environmentToolsUnverified") }}
    </p>
    <p
      v-for="problem in visibleProblems"
      :key="problem"
      class="field-error"
      role="alert"
    >
      {{ $t(problem) }}
    </p>
  </div>
</template>

<style scoped>
.assistant-environment-tools {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.field-error {
  color: var(--color-danger, #b42318);
}
</style>
