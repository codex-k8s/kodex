<script setup lang="ts">
import {
  ChevronDown,
  FileCode2,
  PackageCheck,
  Plus,
  Search,
  ShieldCheck,
} from "@lucide/vue";
import { ref } from "vue";
import { useI18n } from "vue-i18n";

import type { IntegrationPackagePresentation } from "@/features/integrations/ui/model";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{
  packages: readonly IntegrationPackagePresentation[];
  categories: readonly string[];
  search: string;
  category: string;
}>();

const emit = defineEmits<{
  "update:search": [value: string];
  "update:category": [value: string];
  connect: [definitionKey: string];
}>();

const { t } = useI18n();
const expandedKey = ref("");

function toggleDetails(key: string): void {
  expandedKey.value = expandedKey.value === key ? "" : key;
}

function fieldType(field: IntegrationConfigurationField): string {
  if (field.valueType === "URL") return "URL";
  if (field.valueType === "STRING_LIST") return "список строк";
  return "строка";
}
</script>

<template>
  <section class="catalog-panel" aria-labelledby="integration-catalog-title">
    <header class="panel-heading">
      <div>
        <h2 id="integration-catalog-title">
          {{ t("integrationsRedesign.catalogTitle") }}
        </h2>
        <p>{{ t("integrationsRedesign.catalogDescription") }}</p>
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
          :value="category"
          @change="
            emit('update:category', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="">
            {{ t("integrationsRedesign.allCategories") }}
          </option>
          <option v-for="item in categories" :key="item" :value="item">
            {{ item }}
          </option>
        </select>
      </label>
    </div>
    <div v-if="packages.length" class="package-grid">
      <article v-for="item in packages" :key="item.key" class="package-card">
        <header class="package-card__heading">
          <span class="package-icon" aria-hidden="true">
            <PackageCheck :size="20" />
          </span>
          <div class="package-card__identity">
            <h3>{{ item.name }}</h3>
            <span class="package-meta">
              {{ item.category }} ·
              {{
                t(
                  item.builtIn
                    ? "integrationsRedesign.firstParty"
                    : "integrationsRedesign.customPackage",
                )
              }}
            </span>
          </div>
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
        </header>

        <p class="package-description">
          {{ item.description || t("integrations.unavailable") }}
        </p>
        <div class="package-facts">
          <span>{{
            t("integrationsRedesign.connectionCount", {
              count: item.connectionCount,
            })
          }}</span>
          <span>{{
            t("integrationsRedesign.capabilityCount", {
              count: item.capabilityCount,
            })
          }}</span>
          <span v-if="item.approvalCapabilityCount" class="approval-fact">
            <ShieldCheck :size="13" aria-hidden="true" />
            {{
              t("integrationsRedesign.approvalCapabilityCount", {
                count: item.approvalCapabilityCount,
              })
            }}
          </span>
        </div>
        <div class="capability-preview">
          <span
            v-for="capability in item.definition.capabilities.slice(0, 3)"
            :key="capability.key"
            class="capability-token"
          >
            {{ capability.name }} ·
            {{ t(`integrations.risk.${capability.risk}`) }}
          </span>
          <span
            v-if="item.definition.capabilities.length > 3"
            class="capability-more"
          >
            +{{ item.definition.capabilities.length - 3 }}
          </span>
        </div>

        <section
          v-show="expandedKey === item.key"
          class="package-details"
          :aria-label="t('integrationsRedesign.packageDetails')"
          :aria-hidden="expandedKey !== item.key"
        >
          <div class="manifest-facts">
            <span>
              <FileCode2 :size="14" aria-hidden="true" />
              {{ item.definition.schemaVersion }} · v{{
                item.definition.definitionVersion
              }}
            </span>
            <span class="mono">{{ item.definition.adapter }}</span>
            <span class="mono package-digest" :title="item.definition.digest">
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
                <span>{{ t("integrations.risk." + capability.risk) }}</span>
                <span v-if="capability.approvalRequired" class="approval-fact">
                  <ShieldCheck :size="13" aria-hidden="true" /> Human Gate
                </span>
              </div>
              <p>{{ capability.description }}</p>
              <dl class="capability-policy">
                <div>
                  <dt>Operation</dt>
                  <dd class="mono">{{ capability.operation }}</dd>
                </div>
                <div>
                  <dt>Resource scope</dt>
                  <dd class="mono">{{ capability.resourceKind }}</dd>
                </div>
                <div>
                  <dt>Approval policy</dt>
                  <dd class="mono">{{ capability.approvalPolicy }}</dd>
                </div>
              </dl>
              <section class="capability-inputs">
                <h5>Входные поля</h5>
                <dl v-if="capability.inputFields.length" class="field-schema">
                  <div v-for="field in capability.inputFields" :key="field.key">
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
                    </dd>
                  </div>
                </dl>
                <p v-else class="schema-empty">Входные поля отсутствуют.</p>
              </section>
            </li>
          </ul>
        </section>

        <footer class="package-card__actions">
          <button
            class="button"
            type="button"
            :aria-expanded="expandedKey === item.key"
            @click="toggleDetails(item.key)"
          >
            <ChevronDown
              :size="15"
              aria-hidden="true"
              :class="{ 'details-chevron--open': expandedKey === item.key }"
            />
            {{ t("integrationsRedesign.packageDetails") }}
          </button>
          <button
            class="button"
            :class="{ 'button--primary': item.canConnect }"
            type="button"
            :disabled="!item.canConnect"
            :title="
              item.canConnect
                ? undefined
                : t('integrationsRedesign.connectUnavailable')
            "
            @click="emit('connect', item.key)"
          >
            <Plus :size="15" aria-hidden="true" />
            {{ t("integrations.connect") }}
          </button>
        </footer>
      </article>
    </div>
    <div v-else class="catalog-empty">
      <PackageCheck :size="28" aria-hidden="true" />
      <h3>{{ t("integrationsRedesign.noPackages") }}</h3>
      <p>{{ t("integrationsRedesign.noPackagesHint") }}</p>
    </div>

    <div class="zero-connection-notice">
      <ShieldCheck :size="18" aria-hidden="true" />
      <span>{{ t("integrationsRedesign.zeroConnectionsReady") }}</span>
    </div>
  </section>
</template>

<style scoped>
.catalog-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.package-card__heading,
.package-card__actions,
.package-facts,
.catalog-toolbar,
.zero-connection-notice,
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
.package-card h3,
.package-card p,
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
.result-count,
.package-meta,
.package-facts,
.capability-more {
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
.package-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: 12px;
}
.package-card {
  display: flex;
  flex-direction: column;
  min-height: 300px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.package-card__heading {
  align-items: flex-start;
}
.package-icon {
  display: inline-grid;
  place-items: center;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.package-card__identity {
  display: grid;
  flex: 1;
  min-width: 0;
  gap: 2px;
}
.package-description {
  min-height: 58px;
  margin-top: 13px;
}
.package-facts {
  flex-wrap: wrap;
  margin-top: 4px;
}
.package-facts > span {
  padding-right: 9px;
  border-right: 1px solid var(--border);
}
.package-facts > span:last-child {
  border-right: 0;
}
.approval-fact {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--warning);
}
.capability-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
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
.capability-token {
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  font-size: 0.76rem;
}
.package-card__actions {
  justify-content: flex-end;
  margin-top: auto;
  padding-top: 16px;
}
.details-chevron--open {
  transform: rotate(180deg);
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
.zero-connection-notice {
  align-items: flex-start;
  padding: 11px 13px;
  border: 1px solid color-mix(in srgb, var(--success) 28%, var(--border));
  border-radius: 8px;
  color: var(--text-secondary);
  background: var(--success-soft);
}
.zero-connection-notice svg {
  flex: 0 0 auto;
  color: var(--success);
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
  .package-card {
    min-height: 0;
  }
  .package-card__actions .button {
    flex: 1;
  }
  .field-schema > div,
  .capability-policy {
    grid-template-columns: 1fr;
  }
}
</style>
