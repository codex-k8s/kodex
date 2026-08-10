<script setup lang="ts">
import { Copy, Pencil, Plus, Trash2, Unplug } from "@lucide/vue";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useWorkspaceResourcesStore } from "@/features/workspace-resources/store";
import {
  buildAccessSpec,
  buildMutableSpec,
  emptyWorkspaceResourceDraft,
  isWorkspaceDraftBounded,
  type WorkspaceAccessKind,
  type WorkspaceMutableKind,
  type WorkspaceResourceModel,
} from "@/features/workspace-resources/model";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ projectId: string }>();
const store = useWorkspaceResourcesStore();
const { t } = useI18n();
const editorOpen = ref(false);
const editing = ref<WorkspaceResourceModel | null>(null);
const kind = ref<WorkspaceMutableKind | WorkspaceAccessKind>("CHAT");
const copySource = ref<WorkspaceResourceModel | null>(null);
const copyName = ref("");

const form = reactive({
  name: "",
  stableKey: "",
  roomType: "USER" as "USER" | "COORDINATION" | "WORK_CONTROL" | "RUNS",
  workPolicy: "default",
  defaultAgentSelector: "",
  channelSelector: "",
  purpose: "",
  sourceKind: "PROVIDER_CONNECTION_REFERENCE" as
    | "PROVIDER_CONNECTION_REFERENCE"
    | "CREDENTIAL_BINDING",
  sourceSelector: "",
  revision: 1,
  repositorySelector: "",
  workspaceMode: "GIT",
  defaultBranch: "main",
  credentialBindingSelector: "",
  definitionRef: "",
  definitionVersion: 1,
  capabilities: [] as string[],
  credentialBindingSelectors: [] as string[],
  memberActorSelectors: [] as string[],
  roleSelectors: [] as string[],
  allowedTargetRoleSelectors: [] as string[],
  promptProfileSelector: "",
  roleImageRecipeSelector: "",
  repositoryWorkspaceSelectors: [] as string[],
  integrationSelectors: [] as string[],
  contentSha256: "",
  locale: "ru",
});

const isAccess = computed(
  () =>
    kind.value === "TEAM" ||
    kind.value === "ROLE" ||
    kind.value === "PROMPT_PROFILE",
);
const agents = computed(() =>
  store.selectorResources.data.filter((item) => item.kind === "AGENT"),
);
const recipes = computed(() =>
  store.selectorResources.data.filter(
    (item) => item.kind === "ROLE_IMAGE_RECIPE",
  ),
);
const providerConnections = computed(() =>
  store.selectorResources.data.filter(
    (item) => item.kind === "PROVIDER_CONNECTION_REFERENCE",
  ),
);
const repositorySources = computed(() =>
  store.selectorResources.data.filter(
    (item) => item.kind === "REPOSITORY_WORKSPACE",
  ),
);
const integrationSources = computed(() =>
  store.selectorResources.data.filter((item) => item.kind === "INTEGRATION"),
);
const teamSources = computed(() =>
  store.selectorResources.data.filter((item) => item.kind === "TEAM"),
);
const promptSources = computed(() =>
  store.selectorResources.data.filter((item) => item.kind === "PROMPT_PROFILE"),
);
const roles = computed(() =>
  store.access.filter((item) => item.kind === "ROLE"),
);
const prompts = computed(() =>
  store.access.filter((item) => item.kind === "PROMPT_PROFILE"),
);

function resetForm(): void {
  Object.assign(form, { name: "", ...emptyWorkspaceResourceDraft() });
}

function beginCreate(value: WorkspaceMutableKind | WorkspaceAccessKind): void {
  editing.value = null;
  kind.value = value;
  resetForm();
  editorOpen.value = true;
}

function beginEdit(resource: WorkspaceResourceModel): void {
  if (!resource.nextActions.includes("UPDATE")) return;
  editing.value = resource;
  kind.value = resource.kind;
  resetForm();
  form.name = resource.name;
  Object.assign(form, resource.draft);
  editorOpen.value = true;
}

async function save(): Promise<void> {
  if (!isWorkspaceDraftBounded(kind.value, form)) {
    window.alert(t("workspaces.contractLimit"));
    return;
  }
  const resource = editing.value;
  const name = form.name.trim();
  const result = isAccess.value
    ? await store.saveAccess(
        kind.value as WorkspaceAccessKind,
        resource,
        name,
        buildAccessSpec(kind.value as WorkspaceAccessKind, form),
      )
    : await store.saveMutable(
        props.projectId,
        kind.value as WorkspaceMutableKind,
        resource,
        name,
        buildMutableSpec(kind.value as WorkspaceMutableKind, form),
      );
  if (result) editorOpen.value = false;
}

async function changeState(
  resource: WorkspaceResourceModel,
  targetState: "ACTIVE" | "PAUSED",
): Promise<void> {
  const action = targetState === "ACTIVE" ? "ACTIVATE" : "PAUSE";
  if (
    !window.confirm(
      t("workspaces.confirmResourceAction", { action, name: resource.name }),
    )
  )
    return;
  if (
    resource.kind === "TEAM" ||
    resource.kind === "ROLE" ||
    resource.kind === "PROMPT_PROFILE"
  ) {
    await store.executeAccessAction(resource, action);
  } else {
    await store.transitionMutable(resource, targetState);
  }
}

async function archive(resource: WorkspaceResourceModel): Promise<void> {
  if (
    !window.confirm(
      t("workspaces.confirmResourceAction", {
        action: "ARCHIVE",
        name: resource.name,
      }),
    )
  )
    return;
  if (
    resource.kind === "TEAM" ||
    resource.kind === "ROLE" ||
    resource.kind === "PROMPT_PROFILE"
  ) {
    await store.executeAccessAction(resource, "ARCHIVE");
  } else {
    await store.transitionMutable(resource, "ARCHIVED");
  }
}

async function remove(resource: WorkspaceResourceModel): Promise<void> {
  if (
    !window.confirm(
      t("workspaces.confirmResourceAction", {
        action: "DELETE",
        name: resource.name,
      }),
    )
  )
    return;
  if (
    resource.kind === "TEAM" ||
    resource.kind === "ROLE" ||
    resource.kind === "PROMPT_PROFILE"
  ) {
    await store.executeAccessAction(resource, "DELETE");
  } else await store.deleteWorkspaceResource(resource);
}

async function detach(resource: WorkspaceResourceModel): Promise<void> {
  const ownership = resource.ownership;
  if (!ownership || ownership.managedBy !== "git") return;
  if (
    window.confirm(
      t("workspaces.confirmDetachRevision", {
        name: resource.name,
        source: ownership.source,
        revision: ownership.revision,
        drift: ownership.drift,
      }),
    )
  )
    await store.detach(resource);
}

function beginCopy(resource: WorkspaceResourceModel): void {
  copySource.value = resource;
  copyName.value = `${resource.name}-copy`;
}

async function copy(): Promise<void> {
  if (!copySource.value) return;
  const ownership = copySource.value.ownership;
  if (!ownership || ownership.managedBy !== "git") return;
  if (
    !window.confirm(
      t("workspaces.confirmCopyRevision", {
        name: copySource.value.name,
        source: ownership.source,
        revision: ownership.revision,
        drift: ownership.drift,
      }),
    )
  )
    return;
  if (await store.copy(copySource.value, copyName.value.trim()))
    copySource.value = null;
}

watch(
  () => props.projectId,
  (projectId) => {
    editorOpen.value = false;
    editing.value = null;
    copySource.value = null;
    resetForm();
    void store.load(projectId);
  },
  { immediate: true },
);
</script>

<template>
  <section class="panel">
    <header class="panel__header">
      <h2>{{ $t("workspaces.resources") }}</h2>
      <div class="button-row">
        <button
          v-for="resourceKind in [
            'CHAT',
            'CREDENTIAL_BINDING',
            'REPOSITORY_WORKSPACE',
            'INTEGRATION',
            'TEAM',
            'ROLE',
            'PROMPT_PROFILE',
          ] as const"
          :key="resourceKind"
          class="button button--text"
          type="button"
          @click="beginCreate(resourceKind)"
        >
          <Plus :size="14" aria-hidden="true" />{{ resourceKind }}
        </button>
      </div>
    </header>
    <div class="data-table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ $t("common.name") }}</th>
            <th>{{ $t("search.kind") }}</th>
            <th>{{ $t("common.state") }}</th>
            <th>{{ $t("workspaces.credential") }}</th>
            <th>{{ $t("common.managedBy") }}</th>
            <th>{{ $t("common.source") }}</th>
            <th>Drift</th>
            <th>
              <span class="sr-only">{{ $t("common.actions") }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="resource in store.resources.data" :key="resource.id">
            <td class="data-table__name">{{ resource.name }}</td>
            <td>{{ resource.kind }}</td>
            <td><StatusBadge :state="resource.state" /></td>
            <td>
              <template v-if="resource.credential">
                {{ resource.credential.purpose }} · v{{
                  resource.credential.revision
                }}
                ·
                <StatusBadge
                  :state="
                    resource.credential.providerEligible
                      ? 'AVAILABLE'
                      : 'UNAVAILABLE'
                  "
                />
              </template>
              <span v-else>{{ $t("common.noValue") }}</span>
            </td>
            <td>
              <StatusBadge :state="resource.ownership?.managedBy ?? 'ui'" />
            </td>
            <td>
              {{ resource.ownership?.source ?? $t("common.noValue") }}
              ·
              {{ resource.ownership?.revision ?? $t("common.noValue") }}
            </td>
            <td>{{ resource.ownership?.drift ?? $t("common.noValue") }}</td>
            <td>
              <div class="data-table__actions">
                <template>
                  <button
                    v-if="resource.nextActions.includes('DETACH')"
                    class="button button--text"
                    type="button"
                    @click="detach(resource)"
                  >
                    <Unplug :size="14" aria-hidden="true" />{{
                      $t("common.detach")
                    }}
                  </button>
                  <button
                    v-if="resource.nextActions.includes('COPY')"
                    class="button button--text"
                    type="button"
                    @click="beginCopy(resource)"
                  >
                    <Copy :size="14" aria-hidden="true" />{{
                      $t("common.copy")
                    }}
                  </button>
                  <button
                    v-if="resource.nextActions.includes('UPDATE')"
                    class="button button--text"
                    type="button"
                    @click="beginEdit(resource)"
                  >
                    <Pencil :size="14" aria-hidden="true" />{{
                      $t("common.edit")
                    }}
                  </button>
                  <button
                    v-if="resource.nextActions.includes('ACTIVATE')"
                    class="button button--text"
                    type="button"
                    @click="changeState(resource, 'ACTIVE')"
                  >
                    ACTIVATE
                  </button>
                  <button
                    v-if="resource.nextActions.includes('PAUSE')"
                    class="button button--text"
                    type="button"
                    @click="changeState(resource, 'PAUSED')"
                  >
                    PAUSE
                  </button>
                  <button
                    v-if="resource.nextActions.includes('ARCHIVE')"
                    class="button button--text"
                    type="button"
                    @click="archive(resource)"
                  >
                    {{ $t("common.archive") }}
                  </button>
                  <button
                    v-if="resource.nextActions.includes('DELETE')"
                    class="button button--text"
                    type="button"
                    @click="remove(resource)"
                  >
                    <Trash2 :size="14" aria-hidden="true" />{{
                      $t("common.delete")
                    }}
                  </button>
                </template>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <ModalDialog
    :open="editorOpen"
    :title="$t('workspaces.resourceEditor')"
    @close="editorOpen = false"
  >
    <form class="form-grid" @submit.prevent="save">
      <ProblemNotice :problem="store.mutationProblem" />
      <label class="form-field form-field--full"
        ><span>{{ $t("common.name") }}</span
        ><input v-model="form.name" required maxlength="160"
      /></label>

      <template v-if="kind === 'CHAT'">
        <label class="form-field"
          ><span>{{ $t("workspaces.stableKey") }}</span
          ><input v-model="form.stableKey" required maxlength="160"
        /></label>
        <label class="form-field"
          ><span>{{ $t("workspaces.roomType") }}</span
          ><select v-model="form.roomType">
            <option value="USER">USER</option>
            <option value="COORDINATION">COORDINATION</option>
            <option value="WORK_CONTROL">WORK_CONTROL</option>
            <option value="RUNS">RUNS</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("workspaces.workPolicy") }}</span
          ><input v-model="form.workPolicy" required maxlength="80"
        /></label>
        <label class="form-field"
          ><span>{{ $t("people.agent") }}</span
          ><select v-model="form.defaultAgentSelector">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in agents"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field form-field--full"
          ><span>{{ $t("workspaces.chats") }}</span
          ><select v-model="form.channelSelector">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in store.chats"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else-if="kind === 'CREDENTIAL_BINDING'">
        <label class="form-field"
          ><span>purpose</span
          ><input v-model="form.purpose" required maxlength="120"
        /></label>
        <label class="form-field"
          ><span>{{ $t("common.revision") }}</span
          ><input v-model.number="form.revision" required type="number" min="1"
        /></label>
        <label class="form-field form-field--full"
          ><span>{{ $t("search.kind") }}</span
          ><select v-model="form.sourceKind" :required="!editing">
            <option value="PROVIDER_CONNECTION_REFERENCE">
              PROVIDER_CONNECTION_REFERENCE
            </option>
            <option value="CREDENTIAL_BINDING">CREDENTIAL_BINDING</option>
          </select></label
        >
        <label class="form-field form-field--full"
          ><span>{{ $t("providers.accounts") }}</span
          ><select v-model="form.sourceSelector" :required="!editing">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in form.sourceKind === 'PROVIDER_CONNECTION_REFERENCE'
                ? providerConnections
                : store.credentials"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else-if="kind === 'REPOSITORY_WORKSPACE'">
        <label class="form-field form-field--full"
          ><span>{{ $t("workspaces.repositories") }}</span
          ><select v-model="form.repositorySelector" :required="!editing">
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in repositorySources"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("workspaces.workspaceMode") }}</span
          ><input v-model="form.workspaceMode" required maxlength="80"
        /></label>
        <label class="form-field"
          ><span>{{ $t("workspaces.defaultBranch") }}</span
          ><input v-model="form.defaultBranch" required maxlength="255"
        /></label>
        <label class="form-field"
          ><span>{{ $t("workspaces.credential") }}</span
          ><select v-model="form.credentialBindingSelector">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in store.credentials"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else-if="kind === 'INTEGRATION'">
        <label class="form-field"
          ><span>{{ $t("integrations.title") }}</span
          ><select v-model="form.sourceSelector" :required="!editing">
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in integrationSources"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("integrations.definition") }}</span
          ><select
            v-model="form.definitionRef"
            required
            @change="
              form.definitionVersion =
                store.integrationDefinitions.data.find(
                  (item) => item.definitionRef === form.definitionRef,
                )?.version ?? 1
            "
          >
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in store.integrationDefinitions.data"
              :key="item.definitionRef"
              :value="item.definitionRef"
            >
              {{ item.displayName }}
            </option>
          </select></label
        >
        <label class="form-field form-field--full"
          ><span>{{ $t("integrations.capabilities") }}</span
          ><select v-model="form.capabilities" multiple>
            <option
              v-for="item in store.integrationDefinitions.data.find(
                (item) => item.definitionRef === form.definitionRef,
              )?.capabilities ?? []"
              :key="item.name"
              :value="item.name"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field form-field--full"
          ><span>{{ $t("workspaces.credential") }}</span
          ><select v-model="form.credentialBindingSelectors" multiple>
            <option
              v-for="item in store.credentials"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else-if="kind === 'TEAM'">
        <label class="form-field"
          ><span>{{ $t("workspaces.stableKey") }}</span
          ><input v-model="form.stableKey" required maxlength="160"
        /></label>
        <label class="form-field"
          ><span>{{ $t("workspaceTeam.title") }}</span
          ><select v-model="form.sourceSelector" :required="!editing">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in teamSources"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("people.agents") }}</span
          ><select v-model="form.memberActorSelectors" multiple>
            <option
              v-for="item in agents"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("people.roles") }}</span
          ><select v-model="form.roleSelectors" multiple>
            <option
              v-for="item in roles"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else-if="kind === 'ROLE'">
        <label class="form-field"
          ><span>{{ $t("workspaces.stableKey") }}</span
          ><input v-model="form.stableKey" required maxlength="160"
        /></label>
        <label class="form-field"
          ><span>{{ $t("people.capabilities") }}</span
          ><select v-model="form.capabilities" multiple>
            <option
              v-for="value in store.capabilityOptions.data"
              :key="value"
              :value="value"
            >
              {{ value }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("people.roles") }}</span
          ><select v-model="form.allowedTargetRoleSelectors" multiple>
            <option
              v-for="item in roles"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("instructions.title") }}</span
          ><select v-model="form.promptProfileSelector">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in prompts"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("roleImages.title") }}</span
          ><select v-model="form.roleImageRecipeSelector" required>
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in recipes"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("workspaces.credential") }}</span
          ><select v-model="form.credentialBindingSelectors" multiple>
            <option
              v-for="item in store.credentials"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("workspaces.repositories") }}</span
          ><select v-model="form.repositoryWorkspaceSelectors" multiple>
            <option
              v-for="item in store.repositories"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("integrations.title") }}</span
          ><select v-model="form.integrationSelectors" multiple>
            <option
              v-for="item in store.integrations"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <template v-else>
        <label class="form-field"
          ><span>{{ $t("common.revision") }}</span
          ><input v-model.number="form.revision" required type="number" min="1"
        /></label>
        <label class="form-field"
          ><span>{{ $t("instructions.locale") }}</span
          ><input v-model="form.locale" required minlength="2" maxlength="16"
        /></label>
        <label class="form-field form-field--full"
          ><span>contentSha256</span
          ><input
            v-model="form.contentSha256"
            required
            pattern="[a-f0-9]{64}"
            maxlength="64"
        /></label>
        <label class="form-field form-field--full"
          ><span>{{ $t("common.source") }}</span
          ><select v-model="form.sourceSelector" :required="!editing">
            <option value="">{{ $t("common.select") }}</option>
            <option
              v-for="item in promptSources"
              :key="item.selector"
              :value="item.selector"
            >
              {{ item.name }}
            </option>
          </select></label
        >
      </template>

      <div class="button-row form-field--full">
        <button
          class="button button--primary"
          type="submit"
          :disabled="store.mutating"
        >
          {{ editing ? $t("common.save") : $t("common.create") }}
        </button>
      </div>
    </form>
  </ModalDialog>

  <ModalDialog
    :open="copySource !== null"
    :title="$t('common.copy')"
    @close="copySource = null"
  >
    <form class="form-grid" @submit.prevent="copy">
      <label class="form-field"
        ><span>{{ $t("workspaces.newCopyName") }}</span
        ><input v-model="copyName" required maxlength="160"
      /></label>
      <div class="button-row">
        <button
          class="button button--primary"
          type="submit"
          :disabled="store.mutating"
        >
          {{ $t("common.copy") }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
