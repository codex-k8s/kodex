import { defineStore } from 'pinia';

import {
  fetchProjectRepositories,
  fetchProjects,
} from '@/shared/api/staff-gateway';
import type {
  ProjectSummary,
  RepositorySummary,
} from '@/shared/api/generated';
import type { OperatorContext } from '@/shared/api/context';
import type { ApiError } from '@/shared/api/errors';

export const useProjectContextStore = defineStore('project-context', {
  state: () => ({
    projects: [] as ProjectSummary[],
    repositories: [] as RepositorySummary[],
    selectedProjectId: '',
    selectedRepositoryId: '',
    projectNextPageToken: undefined as string | undefined,
    repositoryNextPageToken: undefined as string | undefined,
    isLoadingProjects: false,
    isLoadingRepositories: false,
    error: undefined as ApiError | undefined,
  }),
  getters: {
    selectedProject: (state) =>
      state.projects.find((project) => project.project_id === state.selectedProjectId),
    selectedRepository: (state) =>
      state.repositories.find((repository) => repository.repository_id === state.selectedRepositoryId),
    hasProjects: (state) => state.projects.length > 0,
    hasRepositories: (state) => state.repositories.length > 0,
  },
  actions: {
    async loadProjects(context: OperatorContext) {
      this.isLoadingProjects = true;
      this.error = undefined;
      try {
        const response = await fetchProjects(context, {
          status: 'active',
          pageSize: 50,
        });
        this.projects = response.projects;
        this.projectNextPageToken = response.page.next_page_token;
        if (this.projects.length === 0) {
          this.selectedProjectId = '';
          this.selectedRepositoryId = '';
          this.repositories = [];
          return;
        }
        if (!this.projects.some((project) => project.project_id === this.selectedProjectId)) {
          this.selectedProjectId = this.projects[0].project_id;
          this.selectedRepositoryId = '';
        }
        await this.loadRepositories(context, this.selectedProjectId);
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingProjects = false;
      }
    },
    async selectProject(context: OperatorContext, projectId: string) {
      if (this.selectedProjectId === projectId && this.repositories.length > 0) {
        return;
      }
      this.selectedProjectId = projectId;
      this.selectedRepositoryId = '';
      await this.loadRepositories(context, projectId);
    },
    selectRepository(repositoryId: string | undefined) {
      this.selectedRepositoryId = repositoryId ?? '';
    },
    async loadRepositories(context: OperatorContext, projectId: string) {
      if (!projectId) {
        this.repositories = [];
        this.repositoryNextPageToken = undefined;
        return;
      }
      this.isLoadingRepositories = true;
      this.error = undefined;
      try {
        const response = await fetchProjectRepositories(context, projectId, {
          status: 'active',
          pageSize: 50,
        });
        this.repositories = response.repositories;
        this.repositoryNextPageToken = response.page.next_page_token;
        if (
          this.selectedRepositoryId &&
          !this.repositories.some((repository) => repository.repository_id === this.selectedRepositoryId)
        ) {
          this.selectedRepositoryId = '';
        }
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingRepositories = false;
      }
    },
  },
});
