<script setup lang="ts">
import { CheckCircle2, RefreshCw } from "@lucide/vue";
import { onMounted, reactive } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import type {
  ResolveOwnerGate,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";
import { formatDateTime } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const store = useOperationsStore();
const decisions = reactive<Record<string, ResolveOwnerGate>>({});

function decisionFor(gate: Resource): ResolveOwnerGate {
  const existing = decisions[gate.id];
  if (existing) return existing;
  const initial: ResolveOwnerGate = { decision: "APPROVED", reason: "" };
  decisions[gate.id] = initial;
  return initial;
}

function canResolve(gate: Resource): boolean {
  return (
    gate.spec.ownerGate?.resolvable === true &&
    gate.spec.ownerGate.nextAction === "RESOLVE"
  );
}

async function resolve(gate: Resource): Promise<void> {
  const decision = decisionFor(gate);
  if (decision.reason.trim().length < 3) return;
  const success = await store.resolveOwnerGate(gate, {
    ...decision,
    reason: decision.reason.trim(),
  });
  if (success) decisions[gate.id] = { decision: "APPROVED", reason: "" };
}

async function load(): Promise<void> {
  await Promise.all([store.loadRuns(), store.loadGates()]);
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader :title="$t('runs.title')" :subtitle="$t('runs.subtitle')"
      ><template #actions
        ><button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("runs.run") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.runs.phase"
          :problem="store.runs.problem"
          @retry="store.loadRuns"
        >
          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("runs.playbook") }}</th>
                  <th>{{ $t("runs.policy") }}</th>
                  <th>{{ $t("common.updatedAt") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.runs.data" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    {{
                      item.spec.processRun?.playbookRef ?? $t("common.noValue")
                    }}
                  </td>
                  <td>
                    {{
                      item.spec.processRun?.policyRevision ??
                      $t("common.noValue")
                    }}
                  </td>
                  <td>{{ formatDateTime(item.updatedAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </AsyncPanel>
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("runs.gates") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.gates.phase"
          :problem="store.gates.problem"
          @retry="store.loadGates"
        >
          <div class="panel__body section-stack">
            <article
              v-for="gate in store.gates.data"
              :key="gate.id"
              class="gate-row"
            >
              <div class="gate-row__summary">
                <div>
                  <strong>{{ gate.name }}</strong
                  ><small
                    >{{ $t("runs.expires") }}:
                    {{
                      formatDateTime(gate.spec.ownerGate?.expiresAt, locale)
                    }}</small
                  >
                </div>
                <StatusBadge
                  :state="gate.spec.ownerGate?.decision ?? gate.state"
                />
              </div>
              <form
                v-if="canResolve(gate)"
                class="form-grid gate-row__form"
                @submit.prevent="resolve(gate)"
              >
                <label class="form-field"
                  ><span>{{ $t("runs.decision") }}</span
                  ><select v-model="decisionFor(gate).decision">
                    <option value="APPROVED">{{ $t("runs.approve") }}</option>
                    <option value="REJECTED">{{ $t("runs.reject") }}</option>
                    <option value="CHANGES_REQUESTED">
                      {{ $t("runs.requestChanges") }}
                    </option>
                    <option value="CANCELLED">{{ $t("common.cancel") }}</option>
                  </select></label
                >
                <label class="form-field"
                  ><span>{{ $t("runs.reason") }}</span
                  ><input
                    v-model="decisionFor(gate).reason"
                    required
                    minlength="3"
                    maxlength="512"
                    autocomplete="off"
                /></label>
                <div class="button-row form-field--full">
                  <button
                    class="button button--primary"
                    type="submit"
                    :disabled="store.mutating"
                  >
                    <CheckCircle2 :size="15" aria-hidden="true" />{{
                      $t("runs.resolve")
                    }}
                  </button>
                </div>
              </form>
            </article>
          </div>
        </AsyncPanel>
      </section>
    </div>
  </div>
</template>
