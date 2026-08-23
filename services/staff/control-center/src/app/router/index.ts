import { createRouter, createWebHistory } from "vue-router";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      name: "home",
      component: () => import("@/pages/HomePage.vue"),
    },
    {
      path: "/onboarding",
      name: "onboarding",
      component: () => import("@/pages/OnboardingPage.vue"),
    },
    {
      path: "/assistant",
      name: "assistant",
      component: () => import("@/pages/AssistantPage.vue"),
    },
    {
      path: "/projects",
      name: "projects",
      component: () => import("@/pages/ProjectsPage.vue"),
    },
    {
      path: "/projects/:projectRef",
      name: "project",
      component: () => import("@/pages/ProjectOverviewPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/agents",
      name: "agents",
      component: () => import("@/pages/AgentsPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/agents/:agentRef",
      name: "agent",
      component: () => import("@/pages/AgentDetailPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/workflows",
      name: "workflows",
      component: () => import("@/pages/WorkflowsPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/workflows/:workflowRef",
      name: "workflow",
      component: () => import("@/pages/WorkflowDetailPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/runs/new",
      name: "new-run",
      component: () => import("@/pages/NewRunPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/runs",
      name: "project-runs",
      component: () => import("@/pages/RunsPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/runs",
      name: "runs",
      component: () => import("@/pages/RunsPage.vue"),
    },
    {
      path: "/runs/:runRef",
      name: "run",
      component: () => import("@/pages/RunPage.vue"),
    },
    {
      path: "/projects/:projectRef/files",
      name: "files",
      component: () => import("@/pages/FilesPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/projects/:projectRef/automations",
      name: "automations",
      component: () => import("@/pages/AutomationsPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/integrations",
      name: "integrations",
      component: () => import("@/pages/IntegrationsPage.vue"),
    },
    {
      path: "/decisions",
      name: "decisions",
      component: () => import("@/pages/DecisionsPage.vue"),
    },
    {
      path: "/administration/access",
      name: "access",
      component: () => import("@/pages/AccessPage.vue"),
    },
    {
      path: "/projects/:projectRef/members",
      name: "project-access",
      component: () => import("@/pages/AccessPage.vue"),
      meta: { projectScoped: true },
    },
    {
      path: "/administration",
      name: "administration",
      component: () => import("@/pages/AdministrationPage.vue"),
    },
    {
      path: "/administration/audit",
      name: "audit",
      component: () => import("@/pages/AuditPage.vue"),
    },
    {
      path: "/auth/callback",
      name: "auth-callback",
      component: () => import("@/pages/AuthCallbackPage.vue"),
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
