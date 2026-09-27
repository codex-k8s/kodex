<script setup lang="ts">
import { Copy, Info, FileCode2, Plus, Search, ShieldCheck } from "@lucide/vue";
import { ref, useId } from "vue";
import IntegrationIntegerBounds from "./IntegrationIntegerBounds.vue";
import { useI18n } from "vue-i18n";
import ConfigurationCopyDialog from "@/features/managed-configurations/ConfigurationCopyDialog.vue";
import {
  integrationCopySource,
  type ConfigurationCopySource,
} from "@/features/managed-configurations/copy-source";
import type { ManagedConfiguration } from "@/shared/api/generated/openapi/types.gen";

import type { IntegrationPackagePresentation } from "@/features/integrations/ui/model";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import type { AppProblem } from "@/shared/api/problem";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import EntityIcon from "@/shared/ui/EntityIcon.vue";

const props = defineProps<{
  packages: readonly IntegrationPackagePresentation[];
  categories: readonly string[];
  search: string;
  category: string;
  loading?: boolean;
  hasMore?: boolean;
  problem?: AppProblem;
}>();

const emit = defineEmits<{
  "update:search": [value: string];
  "update:category": [value: string];
  connect: [definitionKey: string];
  copied: [configuration: ManagedConfiguration];
  more: [];
  retry: [];
}>();

const i18n = useI18n();
const { t } = i18n;
const fieldPrefix = `integration-catalog-${useId()}`;
const expandedKey = ref("");
const copySource = ref<ConfigurationCopySource>();
const sentinel = ref<HTMLElement>();
useCursorInfiniteScroll({
  sentinel,
  enabled: () => props.hasMore && !props.loading && !props.problem,
  loadMore: () => emit("more"),
});
function copied(configuration: ManagedConfiguration): void {
  copySource.value = undefined;
  emit("copied", configuration);
}

function toggleDetails(key: string): void {
  expandedKey.value = expandedKey.value === key ? "" : key;
}
function fieldType(field: IntegrationConfigurationField): string {
  if (field.valueType === "URL") return "URL";
  if (field.valueType === "STRING_LIST") return "список строк";
  return "строка";
}
function categoryLabel(category: string): string {
  const key = `integrationsRedesign.packageCategories.${category}`;
  return i18n.te(key) ? t(key) : category;
}
</script>

<template>
  <section class="catalog-panel" aria-labelledby="integration-catalog-title">
    <ConfigurationCopyDialog
      v-if="copySource"
      :source="copySource"
      @close="copySource = undefined"
      @created="copied"
    />
    <header class="panel-heading">
      <div>
        <h2 id="integration-catalog-title">
          {{ t("integrationsRedesign.catalogTitle") }}
        </h2>
      </div>
      <span class="result-count">{{
        t("integrationsRedesign.packageCount", { count: packages.length })
      }}</span>
    </header>

    <div class="catalog-toolbar">
      <label class="search-field">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{
          t("integrationsRedesign.searchPackages")
        }}</span>
        <input
          type="search"
          :id="`${fieldPrefix}-search`"
          :name="`${fieldPrefix}-search`"
          :value="search"
          :placeholder="t('integrationsRedesign.searchPackages')"
          @input="
            emit('update:search', ($event.target as HTMLInputElement).value)
          "
        />
      </label>
      <label class="category-field">
        <span>{{ t("integrationsRedesign.category") }}</span>
        <select
          :id="`${fieldPrefix}-category`"
          :name="`${fieldPrefix}-category`"
          :value="category"
          @change="
            emit('update:category', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="">
            {{ t("integrationsRedesign.allCategories") }}
          </option>
          <option v-for="item in categories" :key="item" :value="item">
            {{ categoryLabel(item) }}
          </option>
        </select>
      </label>
    </div>
    <ProblemNotice v-if="problem" :problem="problem" @retry="emit('retry')" />
    <p v-if="loading && !packages.length" role="status">
      {{ t("common.loading") }}
    </p>
    <div v-if="packages.length" class="package-table-wrap" :aria-busy="loading">
      <table class="package-table">
        <thead>
          <tr>
            <th scope="col">{{ t("integrationsRedesign.table.name") }}</th>
            <th scope="col">
              {{ t("integrationsRedesign.table.description") }}
            </th>
            <th scope="col">{{ t("integrationsRedesign.table.category") }}</th>
            <th scope="col">{{ t("integrationsRedesign.table.state") }}</th>
            <th scope="col">{{ t("integrationsRedesign.table.access") }}</th>
            <th scope="col" class="package-table__actions-heading">
              {{ t("common.actions") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in packages" :key="item.key" class="package-row">
            <td>
              <button
                class="package-name"
                type="button"
                :title="item.name"
                @click="toggleDetails(item.key)"
              >
                <EntityIcon kind="INTEGRATION" />
                <span>{{ item.name }}</span>
              </button>
            </td>
            <td>
              <span class="package-description" :title="item.description">{{
                item.description || t("integrations.unavailable")
              }}</span>
            </td>
            <td>
              <span class="package-cell-text">{{
                categoryLabel(item.category)
              }}</span>
              <small :title="item.definition.digest"
                >v{{ item.definition.definitionVersion }} ·
                {{
                  t(
                    item.builtIn
                      ? "integrationsRedesign.firstParty"
                      : "integrationsRedesign.customPackage",
                  )
                }}</small
              >
            </td>
            <td>
              <StatusBadge
                :state="
                  item.connectionCount
                    ? item.healthyConnectionCount
                      ? 'CONNECTED'
                      : 'DEGRADED'
                    : item.available
                      ? 'AVAILABLE'
                      : 'UNAVAILABLE'
                "
              />
            </td>
            <td>
              <span>{{
                t("integrationsRedesign.connectionCount", {
                  count: item.connectionCount,
                })
              }}</span>
              <small
                :title="
                  item.definition.capabilities
                    .slice(0, 3)
                    .map((capability) => capability.name)
                    .join(' · ')
                "
                >{{
                  t("integrationsRedesign.capabilityCount", {
                    count: item.capabilityCount,
                  })
                }}</small
              >
              <small
                v-if="item.approvalCapabilityCount"
                class="approval-fact"
                >{{
                  t("integrationsRedesign.approvalCapabilityCount", {
                    count: item.approvalCapabilityCount,
                  })
                }}</small
              >
            </td>
            <td>
              <div class="package-actions" role="group" :aria-label="item.name">
                <button
                  v-if="integrationCopySource(item.definition)"
                  class="icon-button"
                  type="button"
                  :title="t('managed.copy')"
                  :aria-label="t('managed.copy')"
                  @click="copySource = integrationCopySource(item.definition)"
                >
                  <Copy :size="17" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  type="button"
                  aria-haspopup="dialog"
                  :title="t('integrationsRedesign.packageDetails')"
                  :aria-label="t('integrationsRedesign.packageDetails')"
                  @click="toggleDetails(item.key)"
                >
                  <Info :size="17" aria-hidden="true" />
                </button>
                <button
                  class="icon-button"
                  :class="{ 'icon-button--primary': item.canConnect }"
                  type="button"
                  :disabled="!item.canConnect"
                  :title="
                    item.canConnect
                      ? t('integrations.connect')
                      : t('integrationsRedesign.connectUnavailable')
                  "
                  :aria-label="t('integrations.connect')"
                  @click="emit('connect', item.key)"
                >
                  <Plus :size="17" aria-hidden="true" />
                </button>
              </div>

              <ModalDialog
                v-if="expandedKey === item.key"
                :title="item.name"
                size="xl"
                @close="expandedKey = ''"
              >
                <section
                  class="package-details"
                  :aria-label="t('integrationsRedesign.packageDetails')"
                >
                  <div class="manifest-facts">
                    <span>
                      <FileCode2 :size="14" aria-hidden="true" />
                      {{ item.definition.schemaVersion }} · v{{
                        item.definition.definitionVersion
                      }}
                    </span>
                    <span class="mono">{{ item.definition.adapter }}</span>
                    <span
                      class="mono package-digest"
                      :title="item.definition.digest"
                    >
                      {{ item.definition.digest.slice(0, 12) }}…
                    </span>
                  </div>
                  <section class="configuration-schema">
                    <h4>Схема подключения</h4>
                    <dl
                      v-if="item.definition.configurationFields.length"
                      class="field-schema"
                    >
                      <div
                        v-for="field in item.definition.configurationFields"
                        :key="field.key"
                      >
                        <dt>
                          <strong>{{ field.label }}</strong>
                          <code>{{ field.key }}</code>
                        </dt>
                        <dd>
                          <span class="type-token">{{ fieldType(field) }}</span>
                          <span>{{
                            field.required ? "обязательное" : "необязательное"
                          }}</span>
                          <span>{{ field.help }}</span>
                          <IntegrationIntegerBounds :field="field" />
                        </dd>
                      </div>
                    </dl>
                    <p v-else class="schema-empty">
                      Публичная конфигурация для подключения не требуется.
                    </p>
                  </section>
                  <ul class="capability-list">
                    <li
                      v-for="capability in item.definition.capabilities"
                      :key="capability.key"
                    >
                      <div class="capability-heading">
                        <strong>{{ capability.name }}</strong>
                        <span>{{
                          t("integrations.risk." + capability.risk)
                        }}</span>
                        <span
                          v-if="capability.approvalRequired"
                          class="approval-fact"
                        >
                          <ShieldCheck :size="13" aria-hidden="true" />
                          {{ t("workflows.humanGate") }}
                        </span>
                      </div>
                      <p>{{ capability.description }}</p>
                      <dl class="capability-policy">
                        <div>
                          <dt>{{ t("managed.fields.operation") }}</dt>
                          <dd class="mono">{{ capability.operation }}</dd>
                        </div>
                        <div>
                          <dt>{{ t("managed.fields.resourceKind") }}</dt>
                          <dd class="mono">{{ capability.resourceKind }}</dd>
                        </div>
                        <div>
                          <dt>{{ t("managed.fields.approval") }}</dt>
                          <dd class="mono">{{ capability.approvalPolicy }}</dd>
                        </div>
                      </dl>
                      <section class="capability-inputs">
                        <h5>Входные поля</h5>
                        <dl
                          v-if="capability.inputFields.length"
                          class="field-schema"
                        >
                          <div
                            v-for="field in capability.inputFields"
                            :key="field.key"
                          >
                            <dt>
                              <strong>{{ field.label }}</strong>
                              <code>{{ field.key }}</code>
                            </dt>
                            <dd>
                              <span class="type-token">{{
                                fieldType(field)
                              }}</span>
                              <span>{{
                                field.required
                                  ? "обязательное"
                                  : "необязательное"
                              }}</span>
                              <span>{{ field.help }}</span>
                              <IntegrationIntegerBounds :field="field" />
                            </dd>
                          </div>
                        </dl>
                        <p v-else class="schema-empty">
                          Входные поля отсутствуют.
                        </p>
                      </section>
                    </li>
                  </ul>
                </section>
                <template #actions>
                  <button
                    class="button button--primary"
                    :disabled="!item.canConnect"
                    @click="
                      expandedKey = '';
                      emit('connect', item.key);
                    "
                  >
                    <Plus :size="15" />{{ t("integrations.connect") }}
                  </button>
                </template>
              </ModalDialog>
            </td>
          </tr>
        </tbody>
      </table>
      <span ref="sentinel" class="catalog-sentinel" aria-hidden="true" />
    </div>
    <div v-else-if="!loading && !problem" class="catalog-empty">
      <EntityIcon kind="INTEGRATION" :size="28" />
      <h3>{{ t("integrationsRedesign.noPackages") }}</h3>
      <p>{{ t("integrationsRedesign.noPackagesHint") }}</p>
    </div>
  </section>
</template>

<style scoped>
.catalog-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.catalog-toolbar,
.manifest-facts,
.capability-heading,
.unavailable-details {
  display: flex;
  align-items: center;
  gap: 10px;
}
.panel-heading {
  justify-content: space-between;
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.catalog-empty h3,
.catalog-empty p {
  margin-bottom: 0;
}
.panel-heading p,
.package-description,
.catalog-empty p,
.projection-note,
.schema-empty {
  color: var(--muted);
}
.projection-note {
  margin: -6px 1px 0;
  font-size: 0.76rem;
}
.result-count {
  color: var(--muted);
  font-size: 0.8rem;
}
.result-count {
  white-space: nowrap;
}
.catalog-toolbar {
  align-items: flex-end;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.search-field {
  position: relative;
  flex: 1 1 300px;
  min-width: 180px;
}
.search-field > svg {
  position: absolute;
  top: 50%;
  left: 10px;
  color: var(--subtle);
  transform: translateY(-50%);
}
.search-field input {
  padding-left: 34px;
}
.category-field {
  display: grid;
  flex: 0 1 240px;
  gap: 5px;
  min-width: 180px;
  color: var(--muted);
  font-size: 0.8rem;
}
.catalog-sentinel {
  display: block;
  height: 1px;
}
.package-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.package-table {
  width: 100%;
  min-width: 1060px;
  table-layout: fixed;
  border-collapse: collapse;
}
.package-table th,
.package-table td {
  padding: 9px 12px;
  text-align: left;
  vertical-align: middle;
}
.package-table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.package-table th:nth-child(1) {
  width: 23%;
}
.package-table th:nth-child(2) {
  width: 26%;
}
.package-table th:nth-child(3) {
  width: 16%;
}
.package-table th:nth-child(4) {
  width: 12%;
}
.package-table th:nth-child(5) {
  width: 15%;
}
.package-table th:nth-child(6) {
  width: 8%;
}
.package-row {
  height: 64px;
  border-top: 1px solid var(--border);
}
.package-row:hover {
  background: var(--panel);
}
.package-name {
  display: flex;
  align-items: center;
  gap: 10px;
  max-width: 100%;
  min-width: 0;
  padding: 0;
  border: 0;
  color: var(--text);
  background: transparent;
  font: inherit;
  font-weight: 600;
  text-align: left;
  cursor: pointer;
}
.package-name:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.package-name span:last-child,
.package-description,
.package-cell-text,
.package-row small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.package-row small {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.75rem;
}
.package-actions {
  display: flex;
  justify-content: flex-end;
  gap: 2px;
}
.package-table__actions-heading {
  text-align: right !important;
}
.package-actions .icon-button--primary {
  color: var(--accent-strong);
}
.approval-fact {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--warning);
}
.package-details {
  display: grid;
  gap: 10px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.configuration-schema,
.capability-inputs {
  display: grid;
  gap: 7px;
}
.configuration-schema h4,
.capability-inputs h5,
.schema-empty {
  margin: 0;
}
.configuration-schema h4 {
  font-size: 0.84rem;
}
.capability-inputs h5 {
  font-size: 0.76rem;
}
.field-schema,
.capability-policy {
  display: grid;
  gap: 6px;
  margin: 0;
}
.field-schema > div {
  display: grid;
  grid-template-columns: minmax(150px, 0.65fr) minmax(0, 1.35fr);
  gap: 10px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
.field-schema dt,
.field-schema dd {
  display: grid;
  min-width: 0;
  gap: 3px;
  margin: 0;
}
.field-schema code,
.field-schema dd {
  overflow-wrap: anywhere;
  font-size: 0.72rem;
}
.field-schema dd {
  color: var(--muted);
}
.type-token {
  width: fit-content;
  padding: 2px 5px;
  border-radius: 5px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-family: var(--font-mono);
}
.manifest-facts {
  flex-wrap: wrap;
  color: var(--muted);
  font-size: 0.76rem;
}
.manifest-facts > span:first-child {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.package-digest {
  overflow: hidden;
  max-width: 140px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.capability-list {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.capability-list li {
  display: grid;
  gap: 4px;
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.capability-list p {
  color: var(--muted);
  font-size: 0.8rem;
}
.capability-list code {
  overflow-wrap: anywhere;
  color: var(--text-secondary);
  font-size: 0.72rem;
}
.capability-policy {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.capability-policy > div {
  min-width: 0;
  padding-top: 6px;
  border-top: 1px solid var(--hairline);
}
.capability-policy dt {
  color: var(--subtle);
  font-size: 0.68rem;
}
.capability-policy dd {
  overflow-wrap: anywhere;
  margin: 3px 0 0;
  font-size: 0.7rem;
}
.schema-empty {
  font-size: 0.76rem;
}
.capability-heading {
  flex-wrap: wrap;
  font-size: 0.78rem;
}
.capability-heading > span:not(.approval-fact) {
  color: var(--muted);
}
.unavailable-details {
  align-items: flex-start;
  color: var(--muted);
  font-size: 0.8rem;
}
.unavailable-details svg {
  flex: 0 0 auto;
}
.catalog-empty {
  display: grid;
  justify-items: center;
  gap: 7px;
  padding: 48px 20px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  text-align: center;
  background: var(--panel);
}
@media (max-width: 700px) {
  .panel-heading,
  .catalog-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .category-field {
    flex-basis: auto;
  }
  .field-schema > div,
  .capability-policy {
    grid-template-columns: 1fr;
  }
}
</style>
