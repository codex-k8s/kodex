import { defineStore } from 'pinia';

import {
  actorTypeOptions,
  isOperatorContextReady,
  scopeTypeOptions,
  type OperatorContext,
} from '@/shared/api/context';

const defaultActorType = import.meta.env.VITE_KODEX_ACTOR_TYPE ?? 'user';
const defaultScopeType = import.meta.env.VITE_KODEX_SCOPE_TYPE ?? 'project';

export const useOperatorContextStore = defineStore('operator-context', {
  state: (): OperatorContext => ({
    actorType: actorTypeOptions.includes(defaultActorType) ? defaultActorType : 'user',
    actorId: import.meta.env.VITE_KODEX_ACTOR_ID ?? '',
    scopeType: scopeTypeOptions.includes(defaultScopeType) ? defaultScopeType : 'project',
    scopeRef: import.meta.env.VITE_KODEX_SCOPE_REF ?? '',
  }),
  getters: {
    isReady: (state) => isOperatorContextReady(state),
    asContext: (state): OperatorContext => ({
      actorType: state.actorType,
      actorId: state.actorId,
      scopeType: state.scopeType,
      scopeRef: state.scopeRef,
    }),
  },
});
