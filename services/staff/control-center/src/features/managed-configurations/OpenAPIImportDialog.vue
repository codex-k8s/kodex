<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { OpenApiInspectionResult } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { createDraft, inspectOpenAPI } from "./api";
import {
  openAPIImportContent,
  type OpenAPIWriteApproval,
  type OpenAPIWriteRisk,
} from "./openapi-import";

const emit = defineEmits<{ close: []; created: [configurationRef: string] }>();
const { t } = useI18n();
const source = ref("");
const inspectedSource = ref("");
const inspection = ref<OpenApiInspectionResult>();
const name = ref("");
const version = ref("1.0.0");
const selectedIds = ref<string[]>([]);
const healthOperationId = ref("");
const risks = ref<Record<string, OpenAPIWriteRisk>>({});
const approvals = ref<Record<string, OpenAPIWriteApproval>>({});
const inspecting = ref(false);
const creating = ref(false);
const problem = ref<AppProblem>();
let inspectionController: AbortController | undefined;
let generation = 0;
let disposed = false;

onBeforeUnmount(() => {
  disposed = true;
  inspectionController?.abort();
  source.value = "";
  inspectedSource.value = "";
});
watch(source, () => {
  if (source.value === inspectedSource.value) return;
  inspectionController?.abort();
  generation += 1;
  inspecting.value = false;
  inspection.value = undefined;
  selectedIds.value = [];
  healthOperationId.value = "";
});

async function loadFile(event: Event): Promise<void> {
  if (!(event.target instanceof HTMLInputElement)) return;
  const file = event.target.files?.[0];
  if (!file) return;
  problem.value = undefined;
  if (file.size > 128 * 1024) {
    problem.value = asProblem(new Error("OpenAPI document size is invalid"));
    return;
  }
  const current = generation;
  try {
    const content = await file.text();
    if (!disposed && generation === current) source.value = content;
  } catch (error) {
    if (!disposed) problem.value = asProblem(error);
  }
}

async function inspect(): Promise<void> {
  if (inspecting.value || creating.value || !source.value) return;
  inspectionController?.abort();
  const controller = new AbortController();
  inspectionController = controller;
  const current = ++generation;
  const document = source.value;
  inspecting.value = true;
  problem.value = undefined;
  try {
    const result = await inspectOpenAPI(document, controller.signal);
    if (
      disposed ||
      controller.signal.aborted ||
      generation !== current ||
      source.value !== document
    )
      return;
    inspection.value = result;
    inspectedSource.value = document;
    name.value = result.title.slice(0, 160);
    version.value = /^\d+\.\d+\.\d+$/.test(result.version)
      ? result.version
      : "1.0.0";
    selectedIds.value = [];
    healthOperationId.value = "";
    risks.value = Object.fromEntries(
      result.operations
        .filter(
          (operation) => operation.candidate && operation.method !== "GET",
        )
        .map((operation) => [operation.operationId, "WRITE"]),
    );
    approvals.value = Object.fromEntries(
      result.operations
        .filter(
          (operation) => operation.candidate && operation.method !== "GET",
        )
        .map((operation) => [operation.operationId, "HUMAN_EACH_EFFECT"]),
    );
  } catch (error) {
    if (!disposed && !controller.signal.aborted && generation === current)
      problem.value = asProblem(error);
  } finally {
    if (generation === current) inspecting.value = false;
  }
}

const content = computed(() => {
  if (!inspection.value) return undefined;
  try {
    return openAPIImportContent({
      source: source.value,
      inspectedSource: inspectedSource.value,
      inspection: inspection.value,
      name: name.value,
      version: version.value,
      selectedIds: selectedIds.value,
      healthOperationId: healthOperationId.value,
      risks: risks.value,
      approvals: approvals.value,
    });
  } catch {
    return undefined;
  }
});

async function create(): Promise<void> {
  if (!content.value || creating.value) return;
  creating.value = true;
  problem.value = undefined;
  try {
    const result = await createDraft("INTEGRATION_DEFINITION", {
      name: name.value.trim(),
      contentFormat: "OPENAPI_IMPORT",
      content: content.value,
    });
    if (
      result.configuration.kind !== "INTEGRATION_DEFINITION" ||
      result.configuration.managedBy !== "UI" ||
      result.revision.contentFormat !== "JSON" ||
      result.revision.state !== "DRAFT"
    )
      throw new Error("OpenAPI import receipt is invalid");
    source.value = "";
    inspectedSource.value = "";
    if (!disposed) emit("created", result.configuration.ref);
  } catch (error) {
    if (!disposed) problem.value = asProblem(error);
  } finally {
    if (!disposed) creating.value = false;
  }
}
</script>

<template>
  <ModalDialog
    :title="t('managed.openapiImport.title')"
    size="xl"
    :busy="creating"
    @close="emit('close')"
  >
    <div class="openapi-import">
      <p>{{ t("managed.openapiImport.intro") }}</p>
      <label>
        <span>{{ t("managed.openapiImport.file") }}</span>
        <input
          name="openapi-import-file"
          type="file"
          accept=".yaml,.yml,.json,application/json,text/yaml"
          :disabled="creating"
          @change="loadFile"
        />
      </label>
      <label>
        <span>{{ t("managed.openapiImport.source") }}</span>
        <textarea
          v-model="source"
          name="openapi-import-source"
          rows="8"
          :disabled="creating"
          spellcheck="false"
          autocomplete="off"
        />
      </label>
      <button
        class="button"
        type="button"
        :disabled="inspecting || creating || !source"
        @click="inspect"
      >
        {{
          inspecting ? t("common.loading") : t("managed.openapiImport.inspect")
        }}
      </button>
      <ProblemNotice v-if="problem" :problem="problem" />
      <template v-if="inspection">
        <p>
          {{ inspection.title }} · {{ inspection.operations.length }}
          {{ t("managed.openapiImport.operations") }}
        </p>
        <label>
          <span>{{ t("managed.openapiImport.name") }}</span>
          <input
            v-model="name"
            name="openapi-import-name"
            type="text"
            maxlength="160"
          />
        </label>
        <label>
          <span>{{ t("managed.openapiImport.version") }}</span>
          <input
            v-model="version"
            name="openapi-import-version"
            type="text"
            maxlength="64"
          />
        </label>
        <fieldset>
          <legend>{{ t("managed.openapiImport.choose") }}</legend>
          <div
            v-for="(operation, index) in inspection.operations"
            :key="
              operation.operationId || `${operation.method}:${operation.path}`
            "
            class="openapi-operation"
          >
            <label>
              <input
                v-model="selectedIds"
                :name="`openapi-import-operation-${index}`"
                type="checkbox"
                :value="operation.operationId"
                :disabled="!operation.candidate || creating"
              />
              <span
                ><code>{{ operation.method }} {{ operation.path }}</code> —
                {{ operation.summary || operation.operationId }}</span
              >
            </label>
            <small
              >{{ operation.serverOrigin
              }}<template v-if="!operation.candidate">
                · {{ t("managed.openapiImport.unavailable") }}:
                {{ operation.reason }}</template
              ></small
            >
            <div
              v-if="
                selectedIds.includes(operation.operationId) &&
                operation.method !== 'GET'
              "
              class="openapi-policy"
            >
              <label>
                <span>{{ t("managed.openapiImport.risk") }}</span>
                <select
                  v-model="risks[operation.operationId]"
                  :name="`openapi-import-risk-${index}`"
                >
                  <option value="WRITE">{{ t("managed.risks.WRITE") }}</option>
                  <option value="SENSITIVE">
                    {{ t("managed.risks.SENSITIVE") }}
                  </option>
                  <option value="DESTRUCTIVE">
                    {{ t("managed.risks.DESTRUCTIVE") }}
                  </option>
                </select>
              </label>
              <label>
                <span>{{ t("managed.openapiImport.approval") }}</span>
                <select
                  v-model="approvals[operation.operationId]"
                  :name="`openapi-import-approval-${index}`"
                >
                  <option value="HUMAN_EACH_EFFECT">
                    {{ t("managed.openapiImport.eachEffect") }}
                  </option>
                  <option value="HUMAN_SCOPED">
                    {{ t("managed.openapiImport.scoped") }}
                  </option>
                </select>
              </label>
            </div>
          </div>
        </fieldset>
        <label>
          <span>{{ t("managed.openapiImport.health") }}</span>
          <select
            v-model="healthOperationId"
            name="openapi-import-health-operation"
          >
            <option value="">
              {{ t("managed.openapiImport.chooseHealth") }}
            </option>
            <option
              v-for="operation in inspection.operations.filter(
                (item) =>
                  item.healthCandidate &&
                  selectedIds.includes(item.operationId),
              )"
              :key="operation.operationId"
              :value="operation.operationId"
            >
              {{ operation.operationId }}
            </option>
          </select>
        </label>
        <p role="status">{{ t("managed.openapiImport.unready") }}</p>
      </template>
    </div>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="creating"
        @click="emit('close')"
      >
        {{ t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        type="button"
        :disabled="!content || creating || inspecting"
        @click="create"
      >
        {{ t("managed.openapiImport.createDraft") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.openapi-import {
  display: grid;
  gap: 12px;
}
.openapi-import label {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.openapi-import textarea {
  width: 100%;
  resize: vertical;
}
.openapi-import fieldset {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.openapi-operation {
  display: grid;
  gap: 6px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.openapi-operation > label {
  display: flex;
  align-items: start;
}
.openapi-operation code {
  overflow-wrap: anywhere;
}
.openapi-policy {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.openapi-policy label {
  flex: 1 1 200px;
}
</style>
