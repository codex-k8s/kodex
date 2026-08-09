<script setup lang="ts">
import { GitCompareArrows, RefreshCw } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import { useInstructionsStore } from "@/features/instructions/store";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const operations = useOperationsStore();
const owner = useInstructionsStore();
const instructionRef = ref("");
const leftVersion = ref(0);
const rightVersion = ref(0);
const versions = computed(() =>
  owner.history.data.map((entry) => entry.resource.version),
);

async function load(): Promise<void> {
  await Promise.all([operations.loadChanges(), owner.loadInstructions()]);
}

async function selectInstruction(): Promise<void> {
  if (!instructionRef.value) return;
  await owner.loadInstructionHistory(instructionRef.value);
  leftVersion.value = versions.value.at(-1) ?? 0;
  rightVersion.value = versions.value[0] ?? 0;
}

async function compare(): Promise<void> {
  if (!instructionRef.value || !leftVersion.value || !rightVersion.value)
    return;
  await owner.loadConfigurationDiff(
    instructionRef.value,
    leftVersion.value,
    rightVersion.value,
  );
}

async function showSource(
  item: (typeof operations.changes.data)[number],
): Promise<void> {
  const supported = [
    "ROLE_DEFINITION",
    "AGENT",
    "INSTRUCTION_SET",
    "PROVIDER_POOL",
  ];
  if (!supported.includes(item.resourceKind)) return;
  await owner.loadConfigurationSource(
    item.resourceId,
    item.resourceKind as
      | "ROLE_DEFINITION"
      | "AGENT"
      | "INSTRUCTION_SET"
      | "PROVIDER_POOL",
  );
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('configuration.title')"
      :subtitle="$t('configuration.ownerSubtitle')"
    >
      <template #actions>
        <button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
      </template>
    </PageHeader>
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("configuration.diff") }}</h2>
        </header>
        <div class="panel__body section-stack">
          <div class="inline-form">
            <label class="form-field">
              <span>{{ $t("configuration.instruction") }}</span>
              <select v-model="instructionRef" @change="selectInstruction">
                <option value="">{{ $t("common.select") }}</option>
                <option
                  v-for="item in owner.instructionSets.data"
                  :key="item.id"
                  :value="item.id"
                >
                  {{ item.name }}
                </option>
              </select>
            </label>
            <label class="form-field">
              <span>{{ $t("configuration.left") }}</span>
              <select v-model.number="leftVersion">
                <option
                  v-for="version in versions"
                  :key="`left-${version}`"
                  :value="version"
                >
                  {{ version }}
                </option>
              </select>
            </label>
            <label class="form-field">
              <span>{{ $t("configuration.right") }}</span>
              <select v-model.number="rightVersion">
                <option
                  v-for="version in versions"
                  :key="`right-${version}`"
                  :value="version"
                >
                  {{ version }}
                </option>
              </select>
            </label>
            <button
              class="button button--primary"
              type="button"
              @click="compare"
            >
              <GitCompareArrows :size="15" aria-hidden="true" />{{
                $t("configuration.compare")
              }}
            </button>
          </div>
          <div v-if="owner.configurationDiff.data" class="diff-list">
            <article
              v-for="(change, index) in owner.configurationDiff.data.changes"
              :key="`${change.path}-${index}`"
            >
              <StatusBadge :state="change.kind" />
              <code>{{ change.path }}</code>
              <span v-if="change.display === 'REDACTED'">{{
                $t("configuration.redacted")
              }}</span>
              <template v-else>
                <del>{{ change.before }}</del
                ><ins>{{ change.after }}</ins>
              </template>
            </article>
          </div>
        </div>
      </section>
      <section v-if="owner.configurationSource.data" class="callout">
        <div>
          <strong>{{ owner.configurationSource.data.displayName }}</strong>
          <span
            >{{ owner.configurationSource.data.managedBy }} ·
            {{ owner.configurationSource.data.source }} ·
            {{ $t("common.revision") }}
            {{ owner.configurationSource.data.sourceRevision }}
            <template v-if="owner.configurationSource.data.sourceSha256">
              · {{ $t("common.sourceDigest") }}
              {{ owner.configurationSource.data.sourceSha256 }}
            </template></span
          >
        </div>
        <StatusBadge :state="owner.configurationSource.data.managedBy" />
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("configuration.changes") }}</h2>
        </header>
        <AsyncPanel
          :phase="operations.changes.phase"
          :problem="operations.changes.problem"
          @retry="operations.loadChanges"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("audit.action") }}</th>
                  <th>{{ $t("audit.resource") }}</th>
                  <th>{{ $t("audit.outcome") }}</th>
                  <th>{{ $t("audit.policy") }}</th>
                  <th>{{ $t("audit.occurredAt") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in operations.changes.data" :key="item.id">
                  <td class="data-table__name">
                    <button
                      v-if="
                        [
                          'ROLE_DEFINITION',
                          'AGENT',
                          'INSTRUCTION_SET',
                          'PROVIDER_POOL',
                        ].includes(item.resourceKind)
                      "
                      class="button button--text"
                      type="button"
                      :aria-label="`${$t('common.details')}: ${item.resourceKind}`"
                      @click="showSource(item)"
                    >
                      <GitCompareArrows :size="15" aria-hidden="true" />{{
                        item.action
                      }}
                    </button>
                    <template v-else>{{ item.action }}</template>
                  </td>
                  <td>{{ item.resourceKind }} · v{{ item.resourceVersion }}</td>
                  <td><StatusBadge :state="item.outcome" /></td>
                  <td>v{{ item.policyRevision }}</td>
                  <td>{{ formatDateTime(item.occurredAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>
    </div>
  </div>
</template>
