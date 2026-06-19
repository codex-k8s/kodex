import { createRouter, createWebHistory } from 'vue-router';

import AppShell from '@/features/layout/AppShell.vue';
import { routeNames } from '@/shared/lib/routes';
import CommandCenterPage from '@/pages/CommandCenterPage.vue';
import ExecutionsPage from '@/pages/ExecutionsPage.vue';
import OwnerInboxPage from '@/pages/OwnerInboxPage.vue';
import ProjectsPage from '@/pages/ProjectsPage.vue';

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: AppShell,
      children: [
        {
          path: '',
          name: routeNames.commandCenter,
          component: CommandCenterPage,
          meta: { titleKey: 'navigation.commandCenter' },
        },
        {
          path: 'inbox',
          name: routeNames.ownerInbox,
          component: OwnerInboxPage,
          meta: { titleKey: 'navigation.ownerInbox' },
        },
        {
          path: 'executions',
          name: routeNames.executions,
          component: ExecutionsPage,
          meta: { titleKey: 'navigation.executions' },
        },
        {
          path: 'projects',
          name: routeNames.projects,
          component: ProjectsPage,
          meta: { titleKey: 'navigation.projects' },
        },
      ],
    },
  ],
});
