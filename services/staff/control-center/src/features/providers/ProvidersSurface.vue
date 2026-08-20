<script setup lang="ts">
import { KeyRound, Plus, RefreshCw, RotateCcw, ShieldOff } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useProvidersStore } from "@/features/providers/store";
import type {
  ProviderConnectionModel,
  ProviderPoolModel,
} from "@/features/providers/model";
import { safeHttpsUrl } from "@/shared/lib/url";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const store = useProvidersStore();
const { t } = useI18n();
const authorizationOpen = ref(false);
const poolOpen = ref(false);
const sourceOpen = ref(false);
const selectedPool = ref<ProviderPoolModel | null>(null);
const authorizationForm = reactive({
  providerRef: "",
  connectionStableKey: "",
  displayName: "",
});
const poolForm = reactive({
  stableKey: "",
  displayName: "",
  policy: "least_used" as "least_used" | "weighted",
  members: {} as Record<string, { selected: boolean; weight: number }>,
});
const availableConnections = computed(() =>
  store.connections.data.filter(
    (item) =>
      item.state === "VALID" ||
      selectedPool.value?.members.some(
        (member) => member.connectionRef === item.connectionRef,
      ),
  ),
);

async function beginPool(value?: ProviderPoolModel): Promise<void> {
  if (value) {
    await store.loadConfigurationSource(value.poolRef);
    if (
      store.configurationSource.phase !== "ready" ||
      store.configurationSource.data?.managedBy === "git"
    ) {
      window.alert(t("providers.gitOwned"));
      return;
    }
  }
  selectedPool.value = value ?? null;
  poolForm.stableKey = value?.stableKey ?? "";
  poolForm.displayName = value?.displayName ?? "";
  poolForm.policy = value?.policy ?? "least_used";
  poolForm.members = Object.fromEntries(
    availableConnections.value.map((connection) => {
      const existing = value?.members.find(
        (member) => member.connectionRef === connection.connectionRef,
      );
      return [
        connection.connectionRef,
        { selected: Boolean(existing), weight: existing?.weight ?? 1 },
      ];
    }),
  );
  poolOpen.value = true;
}

async function showPoolSource(value: ProviderPoolModel): Promise<void> {
  await store.loadConfigurationSource(value.poolRef);
  sourceOpen.value = store.configurationSource.phase === "ready";
}

async function authorize(): Promise<void> {
  const ok = await store.beginAuthorization({
    providerRef: authorizationForm.providerRef,
    connectionStableKey: authorizationForm.connectionStableKey.trim(),
    displayName: authorizationForm.displayName.trim(),
  });
  if (ok) authorizationOpen.value = false;
}

async function savePool(): Promise<void> {
  const members = availableConnections.value
    .filter(
      (connection) => poolForm.members[connection.connectionRef]?.selected,
    )
    .map((connection) => ({
      connectionRef: connection.connectionRef,
      connectionVersion: connection.version,
      connectionGeneration: connection.generation,
      weight: poolForm.members[connection.connectionRef]?.weight ?? 1,
    }));
  if (members.length === 0) return;
  if (members.length > 64) {
    window.alert(t("providers.tooManyMembers"));
    return;
  }
  const value = selectedPool.value;
  const ok = await store.savePool(value, {
    stableKey: poolForm.stableKey.trim(),
    displayName: poolForm.displayName.trim(),
    policy: poolForm.policy,
    members,
  });
  if (ok) poolOpen.value = false;
}

async function archivePool(value: ProviderPoolModel): Promise<void> {
  if (!(await poolIsUiOwned(value))) return;
  if (
    !window.confirm(
      t("providers.confirmArchivePool", { name: value.displayName }),
    )
  )
    return;
  await store.executePoolAction(value, "ARCHIVE");
}

async function deletePool(value: ProviderPoolModel): Promise<void> {
  if (!(await poolIsUiOwned(value))) return;
  if (
    !window.confirm(
      t("providers.confirmDeletePool", { name: value.displayName }),
    )
  )
    return;
  await store.executePoolAction(value, "DELETE");
}

async function poolIsUiOwned(value: ProviderPoolModel): Promise<boolean> {
  await store.loadConfigurationSource(value.poolRef);
  const allowed =
    store.configurationSource.phase === "ready" &&
    store.configurationSource.data?.managedBy === "ui";
  if (!allowed) window.alert(t("providers.gitOwned"));
  return allowed;
}

async function revoke(value: ProviderConnectionModel): Promise<void> {
  if (
    !window.confirm(t("providers.confirmRevoke", { name: value.displayName }))
  )
    return;
  await store.revokeProvider(value);
}

async function reauthorize(value: ProviderConnectionModel): Promise<void> {
  if (
    !window.confirm(
      t("providers.confirmReauthorize", { name: value.displayName }),
    )
  )
    return;
  await store.reauthorizeProvider(value);
}

onMounted(store.loadProviders);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('providers.title')"
      :subtitle="$t('providers.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.loadProviders"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button
          class="button button--primary"
          type="button"
          @click="authorizationOpen = true"
        >
          <KeyRound :size="15" aria-hidden="true" />{{
            $t("providers.connect")
          }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <div
      v-if="store.authorization.data"
      class="callout"
      style="margin-top: 15px"
    >
      <div>
        <strong>{{ $t("providers.authorization") }}</strong
        ><span
          >{{ store.authorization.data.state }} ·
          {{
            store.providers.data.find(
              (provider) =>
                provider.providerRef === store.authorization.data?.providerRef,
            )?.displayName
          }}</span
        >
      </div>
      <div
        v-if="safeHttpsUrl(store.authorization.data.verificationUrl)"
        class="authorization-code"
      >
        <a
          :href="safeHttpsUrl(store.authorization.data.verificationUrl)"
          target="_blank"
          rel="noopener noreferrer"
          >{{ $t("providers.openVerification") }}</a
        ><code v-if="store.authorization.data.userCode">{{
          store.authorization.data.userCode
        }}</code>
      </div>
      <div class="button-row">
        <button
          v-if="store.authorization.data.state === 'CODE_ISSUED'"
          class="button button--secondary"
          type="button"
          @click="store.newAuthorizationCode(store.authorization.data)"
        >
          {{ $t("providers.newCode") }}</button
        ><button
          v-if="
            ['PENDING', 'CODE_ISSUED'].includes(store.authorization.data.state)
          "
          class="button button--danger"
          type="button"
          @click="store.stopAuthorization(store.authorization.data)"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--text"
          type="button"
          @click="
            store.refreshAuthorization(
              store.authorization.data.authorizationRef,
            )
          "
        >
          {{ $t("common.refresh") }}
        </button>
      </div>
    </div>
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("providers.accounts") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.connections.phase"
          :problem="store.connections.problem"
          @retry="store.loadProviders"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("providers.account") }}</th>
                  <th>{{ $t("providers.capabilities") }}</th>
                  <th>{{ $t("providers.capacity") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in store.connections.data"
                  :key="item.connectionRef"
                >
                  <td class="data-table__name">{{ item.displayName }}</td>
                  <td>{{ item.maskedAccount }} · {{ item.maskedLabel }}</td>
                  <td>{{ item.capabilities.join(", ") }}</td>
                  <td>
                    {{
                      item.capacity
                        ? `${item.capacity.usage} / ${item.capacity.limit}`
                        : $t("common.noValue")
                    }}
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div v-if="item.operational" class="data-table__actions">
                      <button
                        class="button button--text"
                        type="button"
                        @click="reauthorize(item)"
                      >
                        <RotateCcw :size="14" aria-hidden="true" />{{
                          $t("providers.reauthorize")
                        }}</button
                      ><button
                        v-if="item.state !== 'REVOKED'"
                        class="button button--text"
                        type="button"
                        @click="revoke(item)"
                      >
                        <ShieldOff :size="14" aria-hidden="true" />{{
                          $t("providers.revoke")
                        }}
                      </button>
                    </div>
                    <span v-else class="text-muted">{{
                      $t("providers.importedReadonly")
                    }}</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div></AsyncPanel
        >
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("providers.pools") }}</h2>
          <button
            class="button button--primary"
            type="button"
            :disabled="availableConnections.length === 0"
            @click="beginPool()"
          >
            <Plus :size="15" aria-hidden="true" />{{
              $t("providers.createPool")
            }}
          </button>
        </header>
        <AsyncPanel
          :phase="store.pools.phase"
          :problem="store.pools.problem"
          @retry="store.loadProviders"
          ><div class="card-grid">
            <article
              v-for="item in store.pools.data"
              :key="item.poolRef"
              class="resource-card"
            >
              <div class="resource-card__header">
                <div>
                  <strong>{{ item.displayName }}</strong
                  ><small>{{ item.stableKey }}</small>
                </div>
                <StatusBadge :state="item.state" />
              </div>
              <dl class="detail-list">
                <div>
                  <dt>{{ $t("providers.policy") }}</dt>
                  <dd>{{ item.policy }}</dd>
                </div>
                <div>
                  <dt>{{ $t("providers.members") }}</dt>
                  <dd>{{ item.memberCount }}</dd>
                </div>
                <div>
                  <dt>{{ $t("providers.eligible") }}</dt>
                  <dd>
                    {{ item.eligibleMemberCount }}
                  </dd>
                </div>
                <div v-if="item.ownership">
                  <dt>{{ $t("common.source") }}</dt>
                  <dd>
                    {{ item.ownership.managedBy }} ·
                    {{ item.ownership.source }} ·
                    {{ item.ownership.revision }} ·
                    {{ item.ownership.drift }}
                  </dd>
                </div>
              </dl>
              <div v-if="item.operational" class="button-row">
                <button
                  class="button button--text"
                  type="button"
                  @click="showPoolSource(item)"
                >
                  {{ $t("common.source") }}
                </button>
                <button
                  class="button button--secondary"
                  type="button"
                  @click="beginPool(item)"
                >
                  {{ $t("common.edit") }}</button
                ><button
                  v-if="item.state !== 'ARCHIVED'"
                  class="button button--text"
                  type="button"
                  @click="archivePool(item)"
                >
                  {{ $t("common.archive") }}
                </button>
                <button
                  class="button button--text"
                  type="button"
                  @click="deletePool(item)"
                >
                  {{ $t("common.delete") }}
                </button>
              </div>
              <p v-else class="text-muted">
                {{ $t("providers.importedReadonly") }}
              </p>
            </article>
          </div></AsyncPanel
        >
      </section>
    </div>

    <ModalDialog
      :open="sourceOpen"
      :title="$t('common.source')"
      @close="sourceOpen = false"
    >
      <dl v-if="store.configurationSource.data" class="detail-list">
        <div>
          <dt>{{ $t("common.managedBy") }}</dt>
          <dd>{{ store.configurationSource.data.managedBy }}</dd>
        </div>
        <div>
          <dt>{{ $t("common.source") }}</dt>
          <dd>{{ store.configurationSource.data.source }}</dd>
        </div>
        <div>
          <dt>{{ $t("common.revision") }}</dt>
          <dd>{{ store.configurationSource.data.sourceRevision }}</dd>
        </div>
        <div>
          <dt>Drift</dt>
          <dd>{{ store.configurationSource.data.drift }}</dd>
        </div>
        <div v-if="store.configurationSource.data.sourceSha256">
          <dt>{{ $t("common.sourceDigest") }}</dt>
          <dd>
            <code>{{ store.configurationSource.data.sourceSha256 }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ $t("common.version", { version: "" }) }}</dt>
          <dd>{{ store.configurationSource.data.version }}</dd>
        </div>
      </dl>
    </ModalDialog>

    <ModalDialog
      :open="authorizationOpen"
      :title="$t('providers.connect')"
      @close="authorizationOpen = false"
      ><form class="form-grid" @submit.prevent="authorize">
        <label class="form-field form-field--full"
          ><span>{{ $t("providers.provider") }}</span
          ><select v-model="authorizationForm.providerRef" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.providers.data"
              :key="item.providerRef"
              :value="item.providerRef"
            >
              {{ item.displayName }}
            </option>
          </select></label
        ><label class="form-field"
          ><span>{{ $t("providers.stableKey") }}</span
          ><input
            v-model="authorizationForm.connectionStableKey"
            required
            maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input
            v-model="authorizationForm.displayName"
            required
            maxlength="160"
        /></label>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("providers.start") }}
          </button>
        </div>
      </form></ModalDialog
    >
    <ModalDialog
      :open="poolOpen"
      :title="
        selectedPool ? $t('providers.editPool') : $t('providers.createPool')
      "
      @close="poolOpen = false"
      ><form class="form-grid" @submit.prevent="savePool">
        <label class="form-field"
          ><span>{{ $t("providers.stableKey") }}</span
          ><input
            v-model="poolForm.stableKey"
            required
            maxlength="160" /></label
        ><label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input
            v-model="poolForm.displayName"
            required
            maxlength="160" /></label
        ><label class="form-field form-field--full"
          ><span>{{ $t("providers.policy") }}</span
          ><select v-model="poolForm.policy">
            <option value="least_used">least_used</option>
            <option value="weighted">weighted</option>
          </select></label
        >
        <fieldset class="selection-list form-field--full">
          <legend>{{ $t("providers.members") }}</legend>
          <label v-for="item in availableConnections" :key="item.connectionRef"
            ><input
              v-model="poolForm.members[item.connectionRef]!.selected"
              type="checkbox" /><span
              >{{ item.displayName }} · {{ item.maskedAccount }}</span
            ><input
              v-model.number="poolForm.members[item.connectionRef]!.weight"
              type="number"
              min="1"
              max="10000"
              :disabled="!poolForm.members[item.connectionRef]!.selected"
              :aria-label="$t('providers.weight')"
          /></label>
        </fieldset>
        <div class="button-row form-field--full">
          <button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form></ModalDialog
    >
  </div>
</template>
