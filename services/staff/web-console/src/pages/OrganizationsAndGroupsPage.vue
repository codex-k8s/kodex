<template>
  <div>
    <PageHeader :title="t('pages.organizationsAndGroups.title')">
      <template #actions>
        <AdaptiveBtn
          variant="tonal"
          icon="mdi-refresh"
          :label="t('common.refresh')"
          :loading="accessGraph.loading"
          @click="reload"
        />
      </template>
    </PageHeader>

    <div class="mt-2 text-body-2 text-medium-emphasis">
      {{ t("pages.organizationsAndGroups.hint") }}
    </div>

    <VAlert v-if="accessGraph.error" type="error" variant="tonal" class="mt-4">
      {{ t(accessGraph.error.messageKey) }}
    </VAlert>

    <VRow class="mt-4" density="compact">
      <VCol v-for="card in summaryCards" :key="card.key" cols="12" md="3">
        <VCard variant="outlined" rounded="lg">
          <VCardText>
            <div class="text-caption text-medium-emphasis">{{ card.label }}</div>
            <div class="text-h5 font-weight-bold mt-2">{{ card.value }}</div>
          </VCardText>
        </VCard>
      </VCol>
    </VRow>

    <VRow class="mt-1" density="compact">
      <VCol cols="12" md="6">
        <VCard variant="outlined">
          <VCardTitle class="text-subtitle-1">{{ t("pages.organizationsAndGroups.organizations") }}</VCardTitle>
          <VCardText>
            <VDataTable :headers="organizationHeaders" :items="organizationRows" :loading="accessGraph.loading" :items-per-page="10" hover>
              <template #no-data>
                <div class="py-8 text-medium-emphasis">{{ t("states.noData") }}</div>
              </template>
            </VDataTable>
          </VCardText>
        </VCard>
      </VCol>

      <VCol cols="12" md="6">
        <VCard variant="outlined">
          <VCardTitle class="text-subtitle-1">{{ t("pages.organizationsAndGroups.groups") }}</VCardTitle>
          <VCardText>
            <VDataTable :headers="groupHeaders" :items="groupRows" :loading="accessGraph.loading" :items-per-page="10" hover>
              <template #item.scope="{ item }">
                <VChip size="small" variant="tonal" :color="item.scope === 'global' ? 'info' : 'secondary'">
                  {{ item.scope_label }}
                </VChip>
              </template>
              <template #no-data>
                <div class="py-8 text-medium-emphasis">{{ t("states.noData") }}</div>
              </template>
            </VDataTable>
          </VCardText>
        </VCard>
      </VCol>

      <VCol cols="12" md="6">
        <VCard variant="outlined">
          <VCardTitle class="text-subtitle-1">{{ t("pages.organizationsAndGroups.organizationMemberships") }}</VCardTitle>
          <VCardText>
            <VDataTable :headers="organizationMembershipHeaders" :items="organizationMembershipRows" :loading="accessGraph.loading" :items-per-page="10" hover>
              <template #item.role="{ item }">
                <VChip size="small" variant="tonal" :color="organizationRoleColor(item.role)">
                  {{ item.role_label }}
                </VChip>
              </template>
              <template #no-data>
                <div class="py-8 text-medium-emphasis">{{ t("states.noData") }}</div>
              </template>
            </VDataTable>
          </VCardText>
        </VCard>
      </VCol>

      <VCol cols="12" md="6">
        <VCard variant="outlined">
          <VCardTitle class="text-subtitle-1">{{ t("pages.organizationsAndGroups.groupMemberships") }}</VCardTitle>
          <VCardText>
            <VDataTable :headers="groupMembershipHeaders" :items="groupMembershipRows" :loading="accessGraph.loading" :items-per-page="10" hover>
              <template #item.scope="{ item }">
                <VChip size="small" variant="tonal" :color="item.scope === 'global' ? 'info' : 'secondary'">
                  {{ item.scope_label }}
                </VChip>
              </template>
              <template #no-data>
                <div class="py-8 text-medium-emphasis">{{ t("states.noData") }}</div>
              </template>
            </VDataTable>
          </VCardText>
        </VCard>
      </VCol>
    </VRow>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";

import AdaptiveBtn from "../shared/ui/AdaptiveBtn.vue";
import PageHeader from "../shared/ui/PageHeader.vue";
import { useAccessGraphStore } from "../features/access-graph/store";

const { t } = useI18n({ useScope: "global" });
const accessGraph = useAccessGraphStore();

const organizationHeaders = [
  { title: t("pages.organizationsAndGroups.name"), key: "name", align: "start" },
  { title: t("pages.organizationsAndGroups.slug"), key: "slug", align: "start" },
  { title: t("pages.organizationsAndGroups.users"), key: "users_count", align: "center" },
  { title: t("pages.organizationsAndGroups.groupsCount"), key: "groups_count", align: "center" },
] as const;

const groupHeaders = [
  { title: t("pages.organizationsAndGroups.name"), key: "name", align: "start" },
  { title: t("pages.organizationsAndGroups.scope"), key: "scope", align: "center" },
  { title: t("pages.organizationsAndGroups.organization"), key: "organization_name", align: "start" },
  { title: t("pages.organizationsAndGroups.users"), key: "members_count", align: "center" },
] as const;

const organizationMembershipHeaders = [
  { title: t("pages.organizationsAndGroups.email"), key: "email", align: "start" },
  { title: t("pages.organizationsAndGroups.organization"), key: "organization_name", align: "start" },
  { title: t("pages.organizationsAndGroups.role"), key: "role", align: "center" },
] as const;

const groupMembershipHeaders = [
  { title: t("pages.organizationsAndGroups.email"), key: "email", align: "start" },
  { title: t("pages.organizationsAndGroups.group"), key: "group_name", align: "start" },
  { title: t("pages.organizationsAndGroups.scope"), key: "scope", align: "center" },
] as const;

const organizationNameById = computed<Record<string, string>>(() => {
  return Object.fromEntries(accessGraph.graph.organizations.map((item) => [item.id, item.name]));
});

const groupById = computed(() => {
  return Object.fromEntries(accessGraph.graph.groups.map((item) => [item.id, item]));
});

const organizationMembershipCountByOrganizationId = computed<Record<string, number>>(() => {
  const counts: Record<string, number> = {};
  for (const item of accessGraph.graph.organization_memberships) {
    counts[item.organization_id] = (counts[item.organization_id] ?? 0) + 1;
  }
  return counts;
});

const groupCountByOrganizationId = computed<Record<string, number>>(() => {
  const counts: Record<string, number> = {};
  for (const item of accessGraph.graph.groups) {
    if (!item.organization_id) {
      continue;
    }
    counts[item.organization_id] = (counts[item.organization_id] ?? 0) + 1;
  }
  return counts;
});

const userGroupMembershipCountByGroupId = computed<Record<string, number>>(() => {
  const counts: Record<string, number> = {};
  for (const item of accessGraph.graph.user_group_memberships) {
    counts[item.group_id] = (counts[item.group_id] ?? 0) + 1;
  }
  return counts;
});

const organizationRows = computed(() => {
  return accessGraph.graph.organizations.map((item) => ({
    id: item.id,
    name: item.name,
    slug: item.slug,
    users_count: organizationMembershipCountByOrganizationId.value[item.id] ?? 0,
    groups_count: groupCountByOrganizationId.value[item.id] ?? 0,
  }));
});

const groupRows = computed(() => {
  return accessGraph.graph.groups.map((item) => ({
    id: item.id,
    name: item.name,
    scope: item.scope,
    scope_label: scopeLabel(item.scope),
    organization_name: item.organization_id ? (organizationNameById.value[item.organization_id] ?? "—") : "—",
    members_count: userGroupMembershipCountByGroupId.value[item.id] ?? 0,
  }));
});

const organizationMembershipRows = computed(() => {
  return accessGraph.graph.organization_memberships.map((item) => ({
    organization_id: item.organization_id,
    email: item.email,
    organization_name: organizationNameById.value[item.organization_id] ?? item.organization_id,
    role: item.role,
    role_label: organizationRoleLabel(item.role),
  }));
});

const groupMembershipRows = computed(() => {
  return accessGraph.graph.user_group_memberships.map((item) => {
    const group = groupById.value[item.group_id];
    return {
      group_id: item.group_id,
      email: item.email,
      group_name: group?.name ?? item.group_id,
      scope: group?.scope ?? "",
      scope_label: scopeLabel(group?.scope ?? ""),
    };
  });
});

const summaryCards = computed(() => [
  { key: "organizations", label: t("pages.organizationsAndGroups.organizations"), value: accessGraph.graph.organizations.length },
  { key: "groups", label: t("pages.organizationsAndGroups.groups"), value: accessGraph.graph.groups.length },
  { key: "organization-memberships", label: t("pages.organizationsAndGroups.organizationMemberships"), value: accessGraph.graph.organization_memberships.length },
  { key: "group-memberships", label: t("pages.organizationsAndGroups.groupMemberships"), value: accessGraph.graph.user_group_memberships.length },
]);

function scopeLabel(scope: string): string {
  if (scope === "global") {
    return t("pages.organizationsAndGroups.scopeGlobal");
  }
  if (scope === "organization") {
    return t("pages.organizationsAndGroups.scopeOrganization");
  }
  return scope || "—";
}

function organizationRoleLabel(role: string): string {
  if (role === "owner") {
    return t("pages.organizationsAndGroups.roleOwner");
  }
  if (role === "admin") {
    return t("pages.organizationsAndGroups.roleAdmin");
  }
  if (role === "member") {
    return t("pages.organizationsAndGroups.roleMember");
  }
  return role || "—";
}

function organizationRoleColor(role: string): string {
  if (role === "owner") {
    return "warning";
  }
  if (role === "admin") {
    return "primary";
  }
  return "secondary";
}

async function reload(): Promise<void> {
  await accessGraph.load();
}

onMounted(async () => {
  await reload();
});
</script>
