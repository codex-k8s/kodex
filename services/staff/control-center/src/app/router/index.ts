import { createRouter, createWebHistory } from "vue-router";

import { lazyPage } from "./lazy-page";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      name: "home",
      component: lazyPage(() => import("@/pages/HomePage.vue")),
    },
    {
      path: "/onboarding",
      name: "onboarding",
      component: lazyPage(() => import("@/pages/OnboardingPage.vue")),
    },
    {
      path: "/projects",
      name: "projects",
      component: lazyPage(() => import("@/pages/ProjectsPage.vue")),
    },
    {
      path: "/projects/:projectRef",
      name: "project",
      component: lazyPage(() => import("@/pages/ProjectOverviewPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/agents",
      name: "agents",
      component: lazyPage(() => import("@/pages/AgentsPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/agents/:agentRef",
      name: "agent",
      component: lazyPage(() => import("@/pages/AgentDetailPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/workflows",
      name: "workflows",
      component: lazyPage(() => import("@/pages/WorkflowsPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/workflows/:workflowRef",
      name: "workflow",
      component: lazyPage(() => import("@/pages/WorkflowDetailPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/runs/new",
      name: "new-run",
      component: lazyPage(() => import("@/pages/NewRunPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/runs",
      name: "project-runs",
      component: lazyPage(() => import("@/pages/RunsPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/runs/:runRef",
      name: "project-run",
      component: lazyPage(() => import("@/pages/RunPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/runs",
      name: "runs",
      component: lazyPage(() => import("@/pages/RunsPage.vue")),
    },
    {
      path: "/runs/:runRef",
      name: "run",
      component: lazyPage(() => import("@/pages/RunPage.vue")),
    },
    {
      path: "/projects/:projectRef/files",
      name: "files",
      component: lazyPage(() => import("@/pages/FilesPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/automations",
      name: "automations",
      component: lazyPage(() => import("@/pages/AutomationsPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/environments",
      name: "runtime-environments",
      component: lazyPage(() => import("@/pages/RuntimeEnvironmentsPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/environments/new",
      name: "runtime-environment-new",
      component: lazyPage(
        () => import("@/pages/RuntimeEnvironmentEditorPage.vue"),
      ),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/environments/:environmentRef",
      name: "runtime-environment",
      component: lazyPage(
        () => import("@/pages/RuntimeEnvironmentEditorPage.vue"),
      ),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/role-images",
      name: "role-images",
      component: lazyPage(() => import("@/pages/RoleImagesPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/role-images/new",
      name: "role-image-new",
      component: lazyPage(() => import("@/pages/RoleImageEditorPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/role-images/:recipeRef",
      name: "role-image",
      component: lazyPage(() => import("@/pages/RoleImageEditorPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/integrations",
      name: "integrations",
      component: lazyPage(() => import("@/pages/IntegrationsPage.vue")),
    },
    {
      path: "/decisions",
      name: "decisions",
      component: lazyPage(() => import("@/pages/DecisionsPage.vue")),
    },
    {
      path: "/administration/access/:section?",
      name: "access",
      component: lazyPage(() => import("@/pages/AccessPage.vue")),
    },
    {
      path: "/projects/:projectRef/members",
      name: "project-access",
      component: lazyPage(() => import("@/pages/AccessPage.vue")),
      meta: { projectScoped: true },
    },
    {
      path: "/administration",
      name: "administration",
      component: lazyPage(() => import("@/pages/AdministrationPage.vue")),
    },
    {
      path: "/administration/audit",
      name: "audit",
      component: lazyPage(() => import("@/pages/AuditPage.vue")),
    },
    {
      path: "/auth/callback",
      name: "auth-callback",
      component: lazyPage(() => import("@/pages/AuthCallbackPage.vue")),
      meta: { public: true },
    },
    { path: "/:pathMatch(.*)*", redirect: "/" },
  ],
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
});

declare module "vue-router" {
  interface RouteMeta {
    public?: boolean;
    projectScoped?: boolean;
  }
}
