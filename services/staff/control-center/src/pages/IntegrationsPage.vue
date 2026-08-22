<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const definitions = computed(() => Object.values(platform.definitions));
const connections = computed(() => Object.values(platform.connections));
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({ definitionKey: "", name: "" });

function openConnection(definitionKey: string): void {
  const definition = platform.definitions[definitionKey];
  if (!definition?.available) return;
  form.definitionKey = definition.key;
  form.name = definition.name;
  problem.value = undefined;
  dialog.value = true;
}

async function submit(): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.connectIntegration({
      definitionKey: form.definitionKey,
      name: form.name,
      publicConfiguration: {},
    });
    dialog.value = false;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function command(
  ref: string,
  action: "TEST" | "ENABLE" | "DISABLE",
): Promise<void> {
  const connection = platform.connections[ref];
  if (!connection) return;
  problem.value = undefined;
  try {
    await platform.changeConnection(connection, action);
  } catch (error) {
    problem.value = asProblem(error);
  }
}

onMounted(() => void platform.loadIntegrations());
</script>

<template>
  <PageFrame
    :title="$t('integrations.title')"
    :subtitle="$t('integrations.subtitle')"
  >
    <ProblemNotice v-if="problem && !dialog" :problem="problem" compact />
    <AsyncState
      :loading="platform.loading.integrations"
      :problem="platform.problems.integrations"
      @retry="platform.loadIntegrations()"
    >
      <section>
        <div class="section-header">
          <h2>{{ $t("integrations.connections") }}</h2>
        </div>
        <div v-if="connections.length" class="entity-list">
          <article
            v-for="connection in connections"
            :key="connection.ref"
            class="entity-row"
          >
            <div>
              <h3>{{ connection.name }}</h3>
              <p>
                {{
                  platform.definitions[connection.definitionKey]?.name ??
                  connection.definitionKey
                }}
                · {{ connection.credentialsHint }}
              </p>
            </div>
            <StatusBadge :state="connection.state" />
            <div class="entity-row__actions">
              <button
                v-if="connection.nextActions.includes('TEST')"
                class="button"
                type="button"
                @click="command(connection.ref, 'TEST')"
              >
                {{ $t("common.test") }}</button
              ><button
                v-if="connection.nextActions.includes('ENABLE')"
                class="button"
                type="button"
                @click="command(connection.ref, 'ENABLE')"
              >
                {{ $t("common.enable") }}</button
              ><button
                v-if="connection.nextActions.includes('DISABLE')"
                class="button button--danger"
                type="button"
                @click="command(connection.ref, 'DISABLE')"
              >
                {{ $t("common.disable") }}
              </button>
            </div>
          </article>
        </div>
        <p v-else class="card">{{ $t("integrations.noConnections") }}</p>
      </section>
      <section>
        <div class="section-header">
          <h2>{{ $t("integrations.catalog") }}</h2>
        </div>
        <div class="card-grid">
          <article
            v-for="definition in definitions"
            :key="definition.key"
            class="card"
          >
            <div class="card-heading">
              <h3>{{ definition.name }}</h3>
              <StatusBadge
                :state="definition.available ? 'READY' : 'UNAVAILABLE'"
              />
            </div>
            <p>{{ definition.description }}</p>
            <p class="muted">
              {{
                definition.capabilities.map((item) => item.name).join(" · ") ||
                $t("common.noData")
              }}
            </p>
            <button
              class="button button--primary"
              type="button"
              :disabled="!definition.available"
              @click="openConnection(definition.key)"
            >
              {{
                definition.available
                  ? $t("integrations.connect")
                  : $t("common.unavailable")
              }}
            </button>
          </article>
        </div>
      </section>
    </AsyncState>
    <ModalDialog
      v-if="dialog"
      :title="$t('integrations.connect')"
      :busy="busy"
      @close="dialog = false"
      ><form id="integration-form" class="form-grid" @submit.prevent="submit">
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="160" autofocus
        /></label>
        <section class="field field--wide card">
          <strong>{{ $t("integrations.credentials") }}</strong>
          <p>{{ $t("integrations.credentialSetup") }}</p>
          <small>{{ $t("integrations.masked") }}</small>
        </section>
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions
        ><button
          class="button"
          type="button"
          :disabled="busy"
          @click="dialog = false"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="integration-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t("integrations.connect") }}
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>

<style scoped>
.card-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.muted {
  color: var(--muted);
}
</style>
