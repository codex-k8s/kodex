<script setup lang="ts">
import {
  Copy,
  GitBranch,
  MessageSquare,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  Unplug,
} from "@lucide/vue";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useProjectsStore } from "@/features/projects/store";
import { useWorkspaceResourcesStore } from "@/features/workspace-resources/store";
import type {
  ChatRoomType,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";
import { resourceOwnership } from "@/shared/lib/resources";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const projects = useProjectsStore();
const resources = useWorkspaceResourcesStore();
const projectId = computed(() => String(route.params.projectId));
const project = computed(() =>
  projects.projects.data.find((item) => item.id === projectId.value),
);
const editorOpen = ref(false);
const repositoryOpen = ref(false);
const chatOpen = ref(false);
const copySource = ref<Resource | null>(null);
const copyName = ref("");
const projectForm = reactive({
  name: "",
  slug: "",
  description: "",
  locale: "ru",
});
const repositoryForm = reactive({
  name: "",
  repositoryRef: "",
  workspaceMode: "ISOLATED_WORKTREE",
  defaultBranch: "main",
  credentialBindingId: "",
});
const chatForm = reactive({
  name: "",
  stableKey: "",
  roomType: "USER" as ChatRoomType,
  workPolicy: "default",
});

watch(
  project,
  (value) => {
    if (!value?.spec.project) return;
    Object.assign(projectForm, {
      name: value.name,
      slug: value.spec.project.slug,
      description: value.spec.project.description,
      locale: value.spec.project.locale,
    });
  },
  { immediate: true },
);

async function load(): Promise<void> {
  await Promise.all([projects.load(), resources.load(projectId.value)]);
}

async function updateProject(): Promise<void> {
  if (
    !project.value ||
    !window.confirm(t("workspaces.confirmSave", { name: project.value.name }))
  )
    return;
  const result = await projects.update(project.value, {
    name: projectForm.name.trim(),
    spec: {
      slug: projectForm.slug.trim(),
      description: projectForm.description.trim(),
      locale: projectForm.locale,
    },
  });
  if (result) editorOpen.value = false;
}

async function deleteProject(): Promise<void> {
  if (
    !project.value ||
    !window.confirm(t("workspaces.confirmDelete", { name: project.value.name }))
  )
    return;
  const result = await projects.remove(project.value);
  if (result) await router.push({ name: "workspaces" });
}

async function createRepository(): Promise<void> {
  const credential = repositoryForm.credentialBindingId || undefined;
  const result = await resources.create({
    kind: "REPOSITORY_WORKSPACE",
    name: repositoryForm.name.trim(),
    parentId: projectId.value,
    spec: {
      repositoryWorkspace: {
        repositoryRef: repositoryForm.repositoryRef.trim(),
        workspaceMode: repositoryForm.workspaceMode.trim(),
        defaultBranch: repositoryForm.defaultBranch.trim(),
        ...(credential ? { credentialBindingId: credential } : {}),
      },
    },
  });
  if (result) repositoryOpen.value = false;
}

async function createChat(): Promise<void> {
  const result = await resources.create({
    kind: "CHAT",
    name: chatForm.name.trim(),
    parentId: projectId.value,
    spec: {
      chat: {
        stableKey: chatForm.stableKey.trim(),
        roomType: chatForm.roomType,
        workPolicy: chatForm.workPolicy.trim(),
      },
    },
  });
  if (result) chatOpen.value = false;
}

async function detach(resource: Resource): Promise<void> {
  if (window.confirm(t("workspaces.confirmDetach", { name: resource.name })))
    await resources.detach(resource);
}

function beginCopy(resource: Resource): void {
  copySource.value = resource;
  copyName.value = `${resource.name}-copy`;
}

async function copy(): Promise<void> {
  if (
    !copySource.value ||
    !window.confirm(
      t("workspaces.confirmCopy", { name: copySource.value.name }),
    )
  )
    return;
  const result = await resources.copy(copySource.value, copyName.value.trim());
  if (result) copySource.value = null;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="project?.name ?? $t('workspaces.detailsTitle')"
      :subtitle="
        project?.spec.project?.description ?? $t('workspaces.subtitle')
      "
    >
      <template #actions>
        <button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
        <button
          v-if="project"
          class="button button--secondary"
          type="button"
          @click="editorOpen = true"
        >
          <Pencil :size="15" aria-hidden="true" />{{ $t("common.edit") }}
        </button>
      </template>
    </PageHeader>
    <AsyncPanel
      :phase="resources.resources.phase"
      :problem="resources.resources.problem"
      @retry="load"
    >
      <div class="section-stack">
        <section class="panel">
          <header class="panel__header">
            <h2>{{ $t("workspaces.repositories") }}</h2>
            <button
              class="button button--primary"
              type="button"
              @click="repositoryOpen = true"
            >
              <Plus :size="15" aria-hidden="true" />{{
                $t("workspaces.createRepository")
              }}
            </button>
          </header>
          <div
            v-if="resources.repositories.length === 0"
            class="state-panel state-panel--quiet"
          >
            {{ $t("common.empty") }}
          </div>
          <div v-else class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("workspaces.repositoryRef") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("common.revision") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in resources.repositories" :key="item.id">
                  <td class="data-table__name">
                    <GitBranch :size="15" aria-hidden="true" />{{ item.name }}
                  </td>
                  <td class="truncate">
                    {{ item.spec.repositoryWorkspace?.repositoryRef }}
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ item.version }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <section class="panel">
          <header class="panel__header">
            <h2>{{ $t("workspaces.chats") }}</h2>
            <button
              class="button button--primary"
              type="button"
              @click="chatOpen = true"
            >
              <Plus :size="15" aria-hidden="true" />{{
                $t("workspaces.createChat")
              }}
            </button>
          </header>
          <div
            v-if="resources.chats.length === 0"
            class="state-panel state-panel--quiet"
          >
            {{ $t("common.empty") }}
          </div>
          <div v-else class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("workspaces.stableKey") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("common.revision") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in resources.chats" :key="item.id">
                  <td class="data-table__name">
                    <MessageSquare :size="15" aria-hidden="true" />{{
                      item.name
                    }}
                  </td>
                  <td>{{ item.spec.chat?.stableKey }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ item.version }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
        <section class="panel">
          <header class="panel__header">
            <h2>{{ $t("workspaces.access") }}</h2>
          </header>
          <div
            v-if="resources.access.length === 0"
            class="state-panel state-panel--quiet"
          >
            {{ $t("common.empty") }}
          </div>
          <div v-else class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("common.managedBy") }}</th>
                  <th>{{ $t("common.source") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in resources.access" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td>
                    <StatusBadge
                      :state="resourceOwnership(item)?.managedBy ?? 'ui'"
                    />
                  </td>
                  <td class="truncate">
                    {{
                      resourceOwnership(item)?.source ?? $t("common.noValue")
                    }}
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>
                    <div
                      v-if="resourceOwnership(item)?.managedBy === 'git'"
                      class="data-table__actions"
                    >
                      <button
                        class="button button--text"
                        type="button"
                        @click="detach(item)"
                      >
                        <Unplug :size="14" aria-hidden="true" />{{
                          $t("common.detach")
                        }}</button
                      ><button
                        class="button button--text"
                        type="button"
                        @click="beginCopy(item)"
                      >
                        <Copy :size="14" aria-hidden="true" />{{
                          $t("common.copy")
                        }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </AsyncPanel>

    <ModalDialog
      :open="editorOpen"
      :title="$t('workspaces.detailsTitle')"
      @close="editorOpen = false"
      ><form @submit.prevent="updateProject">
        <ProblemNotice :problem="projects.mutationProblem" />
        <div class="form-grid" style="margin-top: 14px">
          <label class="form-field"
            ><span>{{ $t("common.name") }}</span
            ><input
              v-model="projectForm.name"
              required
              maxlength="160" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.slug") }}</span
            ><input
              v-model="projectForm.slug"
              required
              pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
              maxlength="80" /></label
          ><label class="form-field form-field--full"
            ><span>{{ $t("workspaces.description") }}</span
            ><textarea
              v-model="projectForm.description"
              maxlength="2000"
            /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.locale") }}</span
            ><select v-model="projectForm.locale">
              <option value="ru">{{ $t("common.russian") }}</option>
              <option value="en">{{ $t("common.english") }}</option>
            </select></label
          >
        </div>
        <div class="button-row">
          <button
            class="button button--danger"
            type="button"
            @click="deleteProject"
          >
            <Trash2 :size="15" aria-hidden="true" />{{
              $t("common.delete")
            }}</button
          ><button
            class="button button--primary"
            type="submit"
            :disabled="projects.mutating"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="repositoryOpen"
      :title="$t('workspaces.createRepository')"
      @close="repositoryOpen = false"
      ><form @submit.prevent="createRepository">
        <ProblemNotice :problem="resources.mutationProblem" />
        <div class="form-grid" style="margin-top: 14px">
          <label class="form-field"
            ><span>{{ $t("common.name") }}</span
            ><input
              v-model="repositoryForm.name"
              required
              maxlength="160" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.defaultBranch") }}</span
            ><input
              v-model="repositoryForm.defaultBranch"
              required
              maxlength="255" /></label
          ><label class="form-field form-field--full"
            ><span>{{ $t("workspaces.repositoryRef") }}</span
            ><input
              v-model="repositoryForm.repositoryRef"
              required
              maxlength="512" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.workspaceMode") }}</span
            ><input
              v-model="repositoryForm.workspaceMode"
              required
              maxlength="80" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.credential") }}</span
            ><select v-model="repositoryForm.credentialBindingId">
              <option value="">{{ $t("common.select") }}</option>
              <option
                v-for="item in resources.credentials"
                :key="item.id"
                :value="item.id"
              >
                {{ item.name }}
              </option>
            </select></label
          >
        </div>
        <div class="button-row">
          <button
            class="button button--secondary"
            type="button"
            @click="repositoryOpen = false"
          >
            {{ $t("common.cancel") }}</button
          ><button
            class="button button--primary"
            type="submit"
            :disabled="resources.mutating"
          >
            {{ $t("common.create") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="chatOpen"
      :title="$t('workspaces.createChat')"
      @close="chatOpen = false"
      ><form @submit.prevent="createChat">
        <ProblemNotice :problem="resources.mutationProblem" />
        <div class="form-grid" style="margin-top: 14px">
          <label class="form-field"
            ><span>{{ $t("common.name") }}</span
            ><input v-model="chatForm.name" required maxlength="160" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.stableKey") }}</span
            ><input
              v-model="chatForm.stableKey"
              required
              maxlength="160" /></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.roomType") }}</span
            ><select v-model="chatForm.roomType">
              <option value="USER">USER</option>
              <option value="COORDINATION">COORDINATION</option>
              <option value="WORK_CONTROL">WORK_CONTROL</option>
              <option value="RUNS">RUNS</option>
            </select></label
          ><label class="form-field"
            ><span>{{ $t("workspaces.workPolicy") }}</span
            ><input v-model="chatForm.workPolicy" required maxlength="80"
          /></label>
        </div>
        <div class="button-row">
          <button
            class="button button--secondary"
            type="button"
            @click="chatOpen = false"
          >
            {{ $t("common.cancel") }}</button
          ><button
            class="button button--primary"
            type="submit"
            :disabled="resources.mutating"
          >
            {{ $t("common.create") }}
          </button>
        </div>
      </form></ModalDialog
    >

    <ModalDialog
      :open="copySource !== null"
      :title="$t('common.copy')"
      @close="copySource = null"
      ><form @submit.prevent="copy">
        <label class="form-field"
          ><span>{{ $t("workspaces.newCopyName") }}</span
          ><input v-model="copyName" required maxlength="160"
        /></label>
        <div class="button-row">
          <button
            class="button button--secondary"
            type="button"
            @click="copySource = null"
          >
            {{ $t("common.cancel") }}</button
          ><button
            class="button button--primary"
            type="submit"
            :disabled="resources.mutating"
          >
            {{ $t("common.copy") }}
          </button>
        </div>
      </form></ModalDialog
    >
  </div>
</template>
