<script setup lang="ts">
import { Check, FlaskConical, Plus, RefreshCw, X } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  type IntegrationView,
  useIntegrationsStore,
} from "@/features/integrations/store";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useIntegrationsStore();
const { t } = useI18n();
const editorOpen = ref(false);
const selected = ref<IntegrationView | null>(null);
const reasonCodes = reactive<Record<string, string>>({});
const form = reactive({
  stableKey: "",
  definitionRef: "",
  connectionRef: "",
  effectKind: "MCP_TOOL" as "MCP_TOOL" | "CLI" | "ENVIRONMENT",
  capabilities: [] as string[],
});
const definition = computed(() =>
  store.integrationDefinitions.data.find(
    (item) => item.definitionRef === form.definitionRef,
  ),
);
const connection = computed(() =>
  store.connections.data.find(
    (item) => item.connectionRef === form.connectionRef,
  ),
);
const capabilities = computed(
  () =>
    definition.value?.capabilities.filter((capability) =>
      connection.value?.capabilities.includes(capability.name),
    ) ?? [],
);

function edit(value?: IntegrationView): void {
  selected.value = value ?? null;
  Object.assign(form, {
    stableKey: value?.stableKey ?? "",
    definitionRef: value?.definitionRef ?? "",
    connectionRef: value?.connectionRef ?? "",
    effectKind: value?.effectKind ?? "MCP_TOOL",
    capabilities: value?.capabilities ?? [],
  });
  editorOpen.value = true;
}

async function save(): Promise<void> {
  if (!definition.value || !connection.value || form.capabilities.length === 0)
    return;
  const value = selected.value;
  const ok = await store.saveIntegrationDraft(value, {
    stableKey: form.stableKey.trim(),
    definitionRef: form.definitionRef,
    connectionRef: form.connectionRef,
    capabilities: form.capabilities,
    effectKind: form.effectKind,
  });
  if (ok) editorOpen.value = false;
}

async function test(value: IntegrationView): Promise<void> {
  const ok = await store.runIntegrationTest(value);
  if (ok) window.alert(t("integrations.testStarted"));
}

async function decide(
  approval: (typeof store.approvals.data)[number],
  decision: "APPROVE" | "REJECT",
): Promise<void> {
  const reasonCode = reasonCodes[approval.approvalRef]?.trim();
  if (
    !reasonCode ||
    !window.confirm(t("integrations.confirmDecision", { decision }))
  )
    return;
  await store.reviewApproval(approval, decision, reasonCode);
}

onMounted(store.loadIntegrations);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('integrations.title')"
      :subtitle="$t('integrations.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadIntegrations"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button
          class="button button--primary"
          type="button"
          :disabled="store.connections.data.length === 0"
          @click="edit()"
        >
          <Plus :size="15" aria-hidden="true" />{{
            $t("integrations.configure")
          }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <div
      v-if="store.integrationTest.data"
      class="callout"
      style="margin-top: 15px"
    >
      <div>
        <strong>{{ $t("integrations.lastTest") }}</strong
        ><span
          >{{ store.integrationTest.data.category }} ·
          {{ store.integrationTest.data.testedAt }}</span
        >
      </div>
      <StatusBadge :state="store.integrationTest.data.category" />
      <button
        v-if="store.integrationTest.data.category === 'PENDING'"
        class="button button--secondary"
        type="button"
        @click="
          store.refreshIntegrationTest(store.integrationTest.data.testRef)
        "
      >
        {{ $t("common.refresh") }}
      </button>
    </div>
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("integrations.catalog") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.integrationDefinitions.phase"
          :problem="store.integrationDefinitions.problem"
          @retry="store.loadIntegrations"
          ><div class="card-grid">
            <article
              v-for="item in store.integrationDefinitions.data"
              :key="item.definitionRef"
              class="resource-card"
            >
              <div class="resource-card__header">
                <strong>{{ item.displayName }}</strong
                ><StatusBadge :state="item.state" />
              </div>
              <div class="chip-list">
                <span
                  v-for="capability in item.capabilities"
                  :key="capability.name"
                  class="chip"
                  >{{ capability.name
                  }}<small v-if="capability.requiresApproval">{{
                    $t("integrations.approvalRequired")
                  }}</small></span
                >
              </div>
            </article>
          </div></AsyncPanel
        >
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("integrations.configurations") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.integrationConfigurations.phase"
          :problem="store.integrationConfigurations.problem"
          @retry="store.loadIntegrations"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("integrations.definition") }}</th>
                  <th>{{ $t("integrations.account") }}</th>
                  <th>{{ $t("integrations.effect") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in store.integrationConfigurations.data"
                  :key="item.configurationRef"
                >
                  <td class="data-table__name">{{ item.stableKey }}</td>
                  <td>
                    {{
                      store.integrationDefinitions.data.find(
                        (definition) =>
                          definition.definitionRef === item.definitionRef,
                      )?.displayName ?? $t("common.noValue")
                    }}
                  </td>
                  <td>
                    {{
                      store.connections.data.find(
                        (connection) =>
                          connection.connectionRef === item.connectionRef,
                      )?.displayName ?? $t("common.noValue")
                    }}
                  </td>
                  <td>{{ item.effectKind }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        class="button button--text"
                        type="button"
                        @click="edit(item)"
                      >
                        {{ $t("common.edit") }}</button
                      ><button
                        class="button button--text"
                        type="button"
                        @click="test(item)"
                      >
                        <FlaskConical :size="14" aria-hidden="true" />{{
                          $t("integrations.test")
                        }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div></AsyncPanel
        >
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("integrations.approvals") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.approvals.phase"
          :problem="store.approvals.problem"
          @retry="store.loadIntegrations"
          ><div class="approval-list">
            <article
              v-for="item in store.approvals.data"
              :key="item.approvalRef"
              class="approval-card"
            >
              <div class="resource-card__header">
                <div>
                  <strong>{{ item.redactedPreview.summary }}</strong
                  ><small>{{ item.redactedPreview.fields.join(" · ") }}</small>
                </div>
                <StatusBadge :state="item.status" />
              </div>
              <form
                v-if="item.status === 'PENDING'"
                class="inline-form"
                @submit.prevent
              >
                <label class="form-field"
                  ><span>{{ $t("integrations.reasonCode") }}</span
                  ><input
                    v-model="reasonCodes[item.approvalRef]"
                    required
                    minlength="1"
                    maxlength="96"
                    autocomplete="off" /></label
                ><button
                  class="button button--primary"
                  type="button"
                  @click="decide(item, 'APPROVE')"
                >
                  <Check :size="15" aria-hidden="true" />{{
                    $t("integrations.approve")
                  }}</button
                ><button
                  class="button button--danger"
                  type="button"
                  @click="decide(item, 'REJECT')"
                >
                  <X :size="15" aria-hidden="true" />{{
                    $t("integrations.reject")
                  }}
                </button>
              </form>
            </article>
          </div></AsyncPanel
        >
      </section>
    </div>
    <ModalDialog
      :open="editorOpen"
      :title="$t('integrations.configure')"
      @close="editorOpen = false"
      ><form class="form-grid" @submit.prevent="save">
        <label class="form-field form-field--full"
          ><span>{{ $t("integrations.stableKey") }}</span
          ><input v-model="form.stableKey" required maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("integrations.definition") }}</span
          ><select v-model="form.definitionRef" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.integrationDefinitions.data.filter(
                (definition) =>
                  definition.state === 'ACTIVE' ||
                  definition.definitionRef === form.definitionRef,
              )"
              :key="item.definitionRef"
              :value="item.definitionRef"
            >
              {{ item.displayName }} · {{ item.state }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("integrations.account") }}</span
          ><select v-model="form.connectionRef" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.connections.data.filter(
                (connection) =>
                  connection.state === 'VALID' ||
                  connection.connectionRef === form.connectionRef,
              )"
              :key="item.connectionRef"
              :value="item.connectionRef"
            >
              {{ item.displayName }} · {{ item.maskedAccount }} ·
              {{ item.state }}
            </option>
          </select></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("integrations.effect") }}</span
          ><select v-model="form.effectKind">
            <option value="MCP_TOOL">MCP_TOOL</option>
            <option value="CLI">CLI</option>
            <option value="ENVIRONMENT">ENVIRONMENT</option>
          </select></label
        >
        <fieldset class="selection-list form-field--full">
          <legend>{{ $t("integrations.capabilities") }}</legend>
          <label v-for="item in capabilities" :key="item.name"
            ><input
              v-model="form.capabilities"
              type="checkbox"
              :value="item.name"
            /><span>{{ item.name }} · {{ item.risk }}</span
            ><small v-if="item.requiresApproval">{{
              $t("integrations.approvalRequired")
            }}</small></label
          >
        </fieldset>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating || form.capabilities.length === 0"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form></ModalDialog
    >
  </div>
</template>
