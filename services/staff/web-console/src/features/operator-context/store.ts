import { defineStore } from 'pinia';

import {
  actorTypeOptions,
  isOperatorContextReady,
  localDevActorHeadersEnabled,
  scopeTypeOptions,
  type OperatorContext,
} from '@/shared/api/context';

const defaultLocalDevActorType = import.meta.env.VITE_KODEX_LOCAL_DEV_ACTOR_TYPE ?? 'user';
const defaultScopeType = import.meta.env.VITE_KODEX_SCOPE_TYPE ?? 'project';

export const useOperatorContextStore = defineStore('operator-context', {
  state: (): OperatorContext => ({
    scopeType: scopeTypeOptions.includes(defaultScopeType) ? defaultScopeType : 'project',
    scopeRef: import.meta.env.VITE_KODEX_SCOPE_REF ?? '',
    localDevActorType: actorTypeOptions.includes(defaultLocalDevActorType) ? defaultLocalDevActorType : 'user',
    localDevActorId: localDevActorHeadersEnabled ? import.meta.env.VITE_KODEX_LOCAL_DEV_ACTOR_ID ?? '' : '',
  }),
  getters: {
    isReady: (state) => isOperatorContextReady(state),
    asContext: (state): OperatorContext => ({
      scopeType: state.scopeType,
      scopeRef: state.scopeRef,
      localDevActorType: state.localDevActorType,
      localDevActorId: state.localDevActorId,
    }),
  },
});
