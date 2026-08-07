<script setup lang="ts">
import { Plus, Trash2 } from "@lucide/vue";
import { reactive, watch } from "vue";

import type {
  RoleImagePackage,
  RoleImageRecipeInput,
  RoleImageRecipeReadback,
  RoleImageTool,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  initial?: RoleImageRecipeReadback | null;
  busy: boolean;
}>();
const emit = defineEmits<{
  submit: [value: { name: string; input: RoleImageRecipeInput }];
}>();
const shaPattern = "[0-9a-f]{64}";
const digestPattern = "sha256:[0-9a-f]{64}";

const form = reactive<{ name: string; input: RoleImageRecipeInput }>({
  name: "",
  input: {
    baseImageReference: "",
    baseImageDigest: "",
    sourceRef: "",
    sourceRevision: "",
    sourceSha256: "",
    contextRef: "",
    contextSha256: "",
    builderSha256: "",
    frontendSha256: "",
    platforms: [{ os: "linux", architecture: "amd64" }],
    packages: [],
    tools: [],
    installationBlock: "",
    toolchainSha256: "",
  },
});

watch(
  () => props.initial,
  (value) => {
    if (!value) {
      Object.assign(form, {
        name: "",
        input: {
          baseImageReference: "",
          baseImageDigest: "",
          sourceRef: "",
          sourceRevision: "",
          sourceSha256: "",
          contextRef: "",
          contextSha256: "",
          builderSha256: "",
          frontendSha256: "",
          platforms: [{ os: "linux", architecture: "amd64" }],
          packages: [],
          tools: [],
          installationBlock: "",
          toolchainSha256: "",
        },
      });
      return;
    }
    form.name = value.recipe.name;
    form.input = structuredClone(value.input);
  },
  { immediate: true },
);

function addPackage(): void {
  const value: RoleImagePackage = {
    manager: "apt",
    name: "",
    version: "",
    digest: "",
    sourceRef: "",
  };
  form.input.packages.push(value);
}
function addTool(): void {
  const value: RoleImageTool = {
    name: "",
    version: "",
    sourceRef: "",
    sha256: "",
  };
  form.input.tools.push(value);
}

function submit(): void {
  emit("submit", {
    name: form.name.trim(),
    input: structuredClone(form.input),
  });
}
</script>

<template>
  <form @submit.prevent="submit">
    <div class="form-grid">
      <label class="form-field"
        ><span>{{ $t("common.name") }}</span
        ><input v-model="form.name" required maxlength="160"
      /></label>
      <label class="form-field"
        ><span>{{ $t("roleImages.platform") }}</span
        ><select v-model="form.input.platforms[0]!.architecture">
          <option value="amd64">amd64</option>
          <option value="arm64">arm64</option>
        </select></label
      >
      <label class="form-field"
        ><span>{{ $t("roleImages.baseImage") }}</span
        ><input
          v-model="form.input.baseImageReference"
          required
          maxlength="512"
      /></label>
      <label class="form-field"
        ><span>{{ $t("roleImages.baseDigest") }}</span
        ><input
          v-model="form.input.baseImageDigest"
          required
          :pattern="digestPattern"
      /></label>
      <label class="form-field"
        ><span>{{ $t("roleImages.sourceRef") }}</span
        ><input v-model="form.input.sourceRef" required maxlength="512"
      /></label>
      <label class="form-field"
        ><span>{{ $t("roleImages.sourceRevision") }}</span
        ><input v-model="form.input.sourceRevision" required maxlength="128"
      /></label>
      <details class="advanced">
        <summary>{{ $t("common.advanced") }}</summary>
        <div class="form-grid">
          <label class="form-field"
            ><span>{{ $t("roleImages.sourceSha") }}</span
            ><input
              v-model="form.input.sourceSha256"
              required
              :pattern="shaPattern"
          /></label>
          <label class="form-field"
            ><span>{{ $t("roleImages.contextRef") }}</span
            ><input v-model="form.input.contextRef" required maxlength="512"
          /></label>
          <label class="form-field"
            ><span>{{ $t("roleImages.contextSha") }}</span
            ><input
              v-model="form.input.contextSha256"
              required
              :pattern="shaPattern"
          /></label>
          <label class="form-field"
            ><span>{{ $t("roleImages.builderSha") }}</span
            ><input
              v-model="form.input.builderSha256"
              required
              :pattern="shaPattern"
          /></label>
          <label class="form-field"
            ><span>{{ $t("roleImages.frontendSha") }}</span
            ><input
              v-model="form.input.frontendSha256"
              required
              :pattern="shaPattern"
          /></label>
          <label class="form-field"
            ><span>{{ $t("roleImages.toolchainSha") }}</span
            ><input
              v-model="form.input.toolchainSha256"
              required
              :pattern="shaPattern"
          /></label>
          <label class="form-field form-field--full"
            ><span>{{ $t("roleImages.installationBlock") }}</span
            ><textarea
              v-model="form.input.installationBlock"
              maxlength="32768"
            />
          </label>
        </div>
      </details>
    </div>
    <section class="panel" style="margin-top: 15px">
      <header class="panel__header">
        <h3>{{ $t("roleImages.packages") }}</h3>
        <button
          class="button button--secondary"
          type="button"
          @click="addPackage"
        >
          <Plus :size="14" aria-hidden="true" />{{
            $t("roleImages.addPackage")
          }}
        </button>
      </header>
      <div v-if="form.input.packages.length" class="panel__body section-stack">
        <div
          v-for="(item, index) in form.input.packages"
          :key="index"
          class="form-grid"
        >
          <label class="form-field"
            ><span>{{ $t("roleImages.manager") }}</span
            ><select v-model="item.manager">
              <option value="apk">apk</option>
              <option value="apt">apt</option>
              <option value="dnf">dnf</option>
              <option value="pip">pip</option>
              <option value="npm">npm</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.packageName") }}</span
            ><input v-model="item.name" required maxlength="128" /></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.packageVersion") }}</span
            ><input v-model="item.version" required maxlength="128" /></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.digest") }}</span
            ><input
              v-model="item.digest"
              required
              :pattern="digestPattern" /></label
          ><label class="form-field form-field--full"
            ><span>{{ $t("roleImages.sourceRef") }}</span
            ><input v-model="item.sourceRef" required maxlength="512" /></label
          ><button
            class="button button--danger"
            type="button"
            @click="form.input.packages.splice(index, 1)"
          >
            <Trash2 :size="14" aria-hidden="true" />{{ $t("common.delete") }}
          </button>
        </div>
      </div>
    </section>
    <section class="panel" style="margin-top: 15px">
      <header class="panel__header">
        <h3>{{ $t("roleImages.tools") }}</h3>
        <button class="button button--secondary" type="button" @click="addTool">
          <Plus :size="14" aria-hidden="true" />{{ $t("roleImages.addTool") }}
        </button>
      </header>
      <div v-if="form.input.tools.length" class="panel__body section-stack">
        <div
          v-for="(item, index) in form.input.tools"
          :key="index"
          class="form-grid"
        >
          <label class="form-field"
            ><span>{{ $t("roleImages.toolName") }}</span
            ><input v-model="item.name" required maxlength="128" /></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.packageVersion") }}</span
            ><input v-model="item.version" required maxlength="128" /></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.sourceRef") }}</span
            ><input v-model="item.sourceRef" required maxlength="512" /></label
          ><label class="form-field"
            ><span>{{ $t("roleImages.sourceSha") }}</span
            ><input
              v-model="item.sha256"
              required
              :pattern="shaPattern" /></label
          ><button
            class="button button--danger"
            type="button"
            @click="form.input.tools.splice(index, 1)"
          >
            <Trash2 :size="14" aria-hidden="true" />{{ $t("common.delete") }}
          </button>
        </div>
      </div>
    </section>
    <div class="button-row">
      <button class="button button--primary" type="submit" :disabled="busy">
        {{ $t("common.save") }}
      </button>
    </div>
  </form>
</template>
