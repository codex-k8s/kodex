<script setup lang="ts">
import {
  Archive,
  Eye,
  Hammer,
  Plus,
  RefreshCw,
  RotateCcw,
  Trash2,
} from "@lucide/vue";
import { onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import RoleImageRecipeForm from "@/features/role-images/RoleImageRecipeForm.vue";
import { useRoleImagesStore } from "@/features/role-images/store";
import type {
  ManageImageBuild,
  ManageRoleImageRecipe,
  Resource,
  RoleImageRecipeInput,
} from "@/shared/api/generated/openapi/types.gen";
import { shortDigest } from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { t } = useI18n();
const store = useRoleImagesStore();
const editorOpen = ref(false);
const editing = ref<Resource | null>(null);
const detailOpen = ref(false);

async function openEdit(resource: Resource): Promise<void> {
  editing.value = resource;
  editorOpen.value = true;
  await store.loadRecipeDetail(resource);
}
async function submit(value: {
  name: string;
  input: RoleImageRecipeInput;
}): Promise<void> {
  const body: ManageRoleImageRecipe = editing.value
    ? {
        action: "UPDATE",
        recipeId: editing.value.id,
        name: value.name,
        input: value.input,
      }
    : { action: "CREATE", name: value.name, input: value.input };
  const success = await store.commandRecipe(body, editing.value ?? undefined);
  if (success) {
    editorOpen.value = false;
    editing.value = null;
  }
}
async function recipeCommand(
  resource: Resource,
  action: ManageRoleImageRecipe["action"],
): Promise<void> {
  if (
    !window.confirm(
      t("roleImages.confirmCommand", { action, name: resource.name }),
    )
  )
    return;
  await store.commandRecipe({ action, recipeId: resource.id }, resource);
}
async function buildCommand(
  resource: Resource,
  action: ManageImageBuild["action"],
): Promise<void> {
  if (
    !window.confirm(
      t("roleImages.confirmCommand", { action, name: resource.name }),
    )
  )
    return;
  await store.commandBuild(resource, action);
}
async function openBuild(resource: Resource): Promise<void> {
  detailOpen.value = true;
  await store.loadBuildDetail(resource);
}
onMounted(store.load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('roleImages.title')"
      :subtitle="$t('roleImages.subtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="store.load"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{
            $t("common.refresh")
          }}</button
        ><button
          class="button button--primary"
          type="button"
          @click="
            editing = null;
            editorOpen = true;
          "
        >
          <Plus :size="15" aria-hidden="true" />{{ $t("roleImages.create") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="store.mutationProblem" />
    <div class="split-layout" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("roleImages.recipes") }}</h2>
        </header>
        <AsyncPanel
          :phase="store.resources.phase"
          :problem="store.resources.problem"
          @retry="store.load"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("roleImages.baseImage") }}</th>
                  <th>
                    <span class="sr-only">{{ $t("common.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in store.recipes" :key="item.id">
                  <td class="data-table__name">{{ item.name }}</td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td class="truncate">
                    {{ item.spec.roleImageRecipe?.input.baseImageReference }}
                  </td>
                  <td>
                    <div class="data-table__actions">
                      <button
                        class="icon-button"
                        type="button"
                        :aria-label="$t('common.edit')"
                        @click="openEdit(item)"
                      >
                        <Eye :size="15" aria-hidden="true" /></button
                      ><button
                        class="icon-button"
                        type="button"
                        :aria-label="$t('roleImages.requestBuild')"
                        @click="recipeCommand(item, 'REQUEST_BUILD')"
                      >
                        <Hammer :size="15" aria-hidden="true" /></button
                      ><button
                        class="icon-button"
                        type="button"
                        :aria-label="$t('common.archive')"
                        @click="recipeCommand(item, 'ARCHIVE')"
                      >
                        <Archive :size="15" aria-hidden="true" /></button
                      ><button
                        class="icon-button"
                        type="button"
                        :aria-label="$t('common.retry')"
                        @click="recipeCommand(item, 'RESTORE')"
                      >
                        <RotateCcw :size="15" aria-hidden="true" /></button
                      ><button
                        class="icon-button"
                        type="button"
                        :aria-label="$t('common.delete')"
                        @click="recipeCommand(item, 'DELETE')"
                      >
                        <Trash2 :size="15" aria-hidden="true" />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table></div
        ></AsyncPanel>
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("roleImages.builds") }}</h2>
        </header>
        <div
          v-if="store.builds.length === 0"
          class="state-panel state-panel--quiet"
        >
          {{ $t("common.empty") }}
        </div>
        <div v-else class="panel__body section-stack">
          <article
            v-for="item in store.builds"
            :key="item.id"
            class="resource-card"
          >
            <div class="resource-card__header">
              <h3>{{ item.name }}</h3>
              <StatusBadge :state="item.spec.imageBuild?.stage ?? item.state" />
            </div>
            <div class="progress">
              <span
                :style="{
                  width: `${item.spec.imageBuild?.progressPercent ?? 0}%`,
                }"
              />
            </div>
            <div class="resource-card__meta">
              <span>{{ item.spec.imageBuild?.progressPercent }}%</span
              ><span>{{ shortDigest(item.spec.imageBuild?.specSha256) }}</span>
            </div>
            <div class="data-table__actions">
              <button
                class="button button--text"
                type="button"
                @click="openBuild(item)"
              >
                {{ $t("common.details") }}</button
              ><button
                class="button button--text"
                type="button"
                @click="buildCommand(item, 'RETRY')"
              >
                {{ $t("common.retry") }}</button
              ><button
                class="button button--text"
                type="button"
                @click="buildCommand(item, 'CANCEL')"
              >
                {{ $t("common.cancel") }}
              </button>
            </div>
          </article>
        </div>
      </section>
    </div>
    <ModalDialog
      :open="editorOpen"
      :title="editing ? $t('common.edit') : $t('roleImages.createTitle')"
      @close="
        editorOpen = false;
        editing = null;
      "
      ><ProblemNotice :problem="store.mutationProblem" /><AsyncPanel
        v-if="editing"
        :phase="store.recipeDetail.phase"
        :problem="store.recipeDetail.problem"
        @retry="editing && store.loadRecipeDetail(editing)"
        ><RoleImageRecipeForm
          :initial="store.recipeDetail.data"
          :busy="store.mutating"
          @submit="submit" /></AsyncPanel
      ><RoleImageRecipeForm v-else :busy="store.mutating" @submit="submit"
    /></ModalDialog>
    <ModalDialog
      :open="detailOpen"
      :title="$t('common.details')"
      @close="detailOpen = false"
      ><AsyncPanel
        :phase="store.buildDetail.phase"
        :problem="store.buildDetail.problem"
        ><div v-if="store.buildDetail.data" class="section-stack">
          <div class="resource-card__header">
            <span>{{ $t("common.name") }}</span
            ><strong>{{ store.buildDetail.data.name }}</strong>
          </div>
          <div class="resource-card__header">
            <span>{{ $t("roleImages.stage") }}</span
            ><StatusBadge
              :state="
                store.buildDetail.data.spec.imageBuild?.stage ??
                store.buildDetail.data.state
              "
            />
          </div>
          <div class="resource-card__header">
            <span>{{ $t("common.revision") }}</span
            ><strong>{{ store.buildDetail.data.version }}</strong>
          </div>
        </div></AsyncPanel
      ></ModalDialog
    >
  </div>
</template>
