<script setup lang="ts">
import { Link2, Plus, RefreshCw, Unlink } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useOwnerControlStore } from "@/features/owner-control/store";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useOwnerControlStore();
const { t } = useI18n();
const createOpen = ref(false);
const selectedSelector = ref("");
const createForm = reactive({ displayName: "", slugIntent: "" });
const activeTeams = computed(() =>
  store.teams.data.filter((team) => team.status === "ACTIVE"),
);

async function addTeam(): Promise<void> {
  const ok = await store.addTeam({
    displayName: createForm.displayName.trim(),
    slugIntent: createForm.slugIntent.trim(),
  });
  if (ok) {
    createOpen.value = false;
    createForm.displayName = "";
    createForm.slugIntent = "";
  }
}

async function bind(): Promise<void> {
  if (!selectedSelector.value) return;
  const binding = store.teamBinding.data;
  if (
    !window.confirm(
      t(binding ? "workspaceTeam.confirmRelink" : "workspaceTeam.confirmLink"),
    )
  )
    return;
  if (binding) {
    await store.rebindTeam(
      {
        selector: selectedSelector.value,
        expectedGeneration: binding.mappingGeneration,
      },
      binding.mappingVersion,
    );
  } else {
    await store.bindTeam(selectedSelector.value);
  }
}

async function unlink(): Promise<void> {
  const binding = store.teamBinding.data;
  if (!binding || !window.confirm(t("workspaceTeam.confirmUnlink"))) return;
  await store.removeTeamBinding(
    binding.mappingVersion,
    binding.mappingGeneration,
  );
}

onMounted(store.loadTeams);
</script>

<template>
  <section class="panel">
    <header class="panel__header">
      <div>
        <h2>{{ $t("workspaceTeam.title") }}</h2>
        <small>{{ $t("workspaceTeam.subtitle") }}</small>
      </div>
      <div class="button-row">
        <button
          class="button button--secondary"
          type="button"
          @click="store.loadTeams"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
        <button
          class="button button--primary"
          type="button"
          @click="createOpen = true"
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("workspaceTeam.create") }}
        </button>
      </div>
    </header>
    <ProblemNotice :problem="store.mutationProblem" />
    <div v-if="store.teamOperation.data" class="callout panel__body">
      <div>
        <strong>{{ $t("workspaceTeam.operation") }}</strong>
        <span
          >{{ store.teamOperation.data.action }} ·
          {{ store.teamOperation.data.updatedAt }}</span
        >
      </div>
      <StatusBadge :state="store.teamOperation.data.state" />
    </div>
    <AsyncPanel
      :phase="store.teams.phase"
      :problem="store.teams.problem"
      @retry="store.loadTeams"
    >
      <div class="panel__body section-stack">
        <div v-if="store.teamBinding.data" class="summary-grid">
          <div class="summary-card">
            <small>{{ $t("workspaceTeam.current") }}</small>
            <strong>{{ store.teamBinding.data.team.displayName }}</strong>
            <StatusBadge :state="store.teamBinding.data.state" />
          </div>
          <div class="summary-card">
            <small>{{ $t("workspaceTeam.providerStatus") }}</small>
            <strong>{{ store.teamBinding.data.team.slug }}</strong>
            <span>{{
              $t("common.version", {
                version: store.teamBinding.data.providerEffectVersion,
              })
            }}</span>
          </div>
        </div>
        <form class="inline-form" @submit.prevent="bind">
          <label class="form-field">
            <span>{{ $t("workspaceTeam.select") }}</span>
            <select v-model="selectedSelector" required>
              <option value="">{{ $t("common.select") }}</option>
              <option
                v-for="team in activeTeams"
                :key="team.selector"
                :value="team.selector"
              >
                {{ team.displayName }} · {{ team.slug }}
              </option>
            </select>
          </label>
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating || !selectedSelector"
          >
            <Link2 :size="15" aria-hidden="true" />{{
              store.teamBinding.data
                ? $t("workspaceTeam.relink")
                : $t("workspaceTeam.link")
            }}
          </button>
          <button
            v-if="store.teamBinding.data"
            class="button button--danger"
            type="button"
            :disabled="store.mutating"
            @click="unlink"
          >
            <Unlink :size="15" aria-hidden="true" />{{
              $t("workspaceTeam.unlink")
            }}
          </button>
        </form>
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("workspaceTeam.slug") }}</th>
                <th>{{ $t("common.state") }}</th>
                <th>{{ $t("common.updatedAt") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="team in store.teams.data" :key="team.selector">
                <td class="data-table__name">{{ team.displayName }}</td>
                <td>{{ team.slug }}</td>
                <td><StatusBadge :state="team.status" /></td>
                <td>{{ team.observedAt }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </AsyncPanel>
  </section>

  <ModalDialog
    :open="createOpen"
    :title="$t('workspaceTeam.create')"
    @close="createOpen = false"
  >
    <form class="form-grid" @submit.prevent="addTeam">
      <label class="form-field form-field--full">
        <span>{{ $t("common.name") }}</span>
        <input
          v-model="createForm.displayName"
          required
          minlength="2"
          maxlength="120"
          autocomplete="off"
        />
      </label>
      <label class="form-field form-field--full">
        <span>{{ $t("workspaceTeam.slug") }}</span>
        <input
          v-model="createForm.slugIntent"
          required
          minlength="2"
          maxlength="63"
          pattern="[a-z0-9-]+"
          autocomplete="off"
        />
      </label>
      <div class="button-row form-field--full">
        <button
          class="button button--primary"
          type="submit"
          :disabled="store.mutating"
        >
          {{ $t("common.create") }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
