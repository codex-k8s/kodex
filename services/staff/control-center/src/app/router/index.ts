import { createRouter, createWebHistory } from "vue-router";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      name: "overview",
      component: () => import("@/pages/OverviewPage.vue"),
    },
    {
      path: "/workspaces",
      name: "workspaces",
      component: () => import("@/pages/WorkspacesPage.vue"),
    },
    {
      path: "/workspaces/:projectId",
      name: "workspace",
      component: () => import("@/pages/WorkspacePage.vue"),
    },
    {
      path: "/role-images",
      name: "role-images",
      component: () => import("@/pages/RoleImagesPage.vue"),
    },
    {
      path: "/automations",
      name: "automations",
      component: () => import("@/pages/SchedulesPage.vue"),
    },
    {
      path: "/runs",
      name: "runs",
      component: () => import("@/pages/RunsPage.vue"),
    },
    {
      path: "/operations/incidents",
      name: "incidents",
      component: () => import("@/pages/IncidentsPage.vue"),
    },
    {
      path: "/operations/backups",
      name: "backups",
      component: () => import("@/pages/BackupsPage.vue"),
    },
    {
      path: "/operations/audit",
      name: "audit",
      component: () => import("@/pages/AuditPage.vue"),
    },
    {
      path: "/operations/configuration",
      name: "configuration",
      component: () => import("@/pages/ConfigurationPage.vue"),
    },
    {
      path: "/operations/diagnostics",
      name: "diagnostics",
      component: () => import("@/pages/DiagnosticsPage.vue"),
    },
    {
      path: "/search",
      name: "search",
      component: () => import("@/pages/SearchPage.vue"),
    },
    {
      path: "/auth/callback",
      name: "auth-callback",
      component: () => import("@/pages/AuthCallbackPage.vue"),
      meta: { public: true },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
  scrollBehavior: () => ({ top: 0 }),
});
