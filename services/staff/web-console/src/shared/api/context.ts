import type { ActorTypeHeader, AgentScopeType, ScopeType } from './generated';
import { createRequestId } from '@/shared/lib/request';

export type OperatorContext = {
  scopeType: ScopeType;
  scopeRef: string;
  localDevActorType: ActorTypeHeader;
  localDevActorId: string;
};

export type GatewayHeaders = {
  'X-Kodex-Request-Id': string;
  'X-Kodex-Actor-Type'?: ActorTypeHeader;
  'X-Kodex-Actor-Id'?: string;
  'X-Kodex-Trace-Id'?: string;
  'X-Kodex-Session-Id'?: string;
};

export const actorTypeOptions: ActorTypeHeader[] = ['user', 'service', 'agent', 'external_account'];
export const scopeTypeOptions: ScopeType[] = ['platform', 'organization', 'project', 'repository', 'service'];
export const agentScopeTypeOptions: AgentScopeType[] = ['platform', 'organization', 'project', 'repository'];
export const localDevActorHeadersEnabled =
  import.meta.env.DEV && import.meta.env.VITE_ENABLE_LOCAL_DEV_ACTOR_HEADERS === 'true';

export function isOperatorContextReady(context: OperatorContext): boolean {
  if (context.scopeRef.trim().length === 0) {
    return false;
  }
  return !localDevActorHeadersEnabled || context.localDevActorId.trim().length > 0;
}

export function isLocalDevActorHeadersEnabled(): boolean {
  return localDevActorHeadersEnabled;
}

export function gatewayHeaders(context: OperatorContext): GatewayHeaders {
  const headers: GatewayHeaders = {
    'X-Kodex-Request-Id': createRequestId(),
  };
  if (localDevActorHeadersEnabled) {
    headers['X-Kodex-Actor-Type'] = context.localDevActorType;
    headers['X-Kodex-Actor-Id'] = context.localDevActorId.trim();
  }
  return headers;
}

export function isAgentScopeType(value: ScopeType): value is AgentScopeType {
  return agentScopeTypeOptions.includes(value as AgentScopeType);
}

export function operationHeaders<THeaders extends object>(context: OperatorContext): THeaders {
  // OpenAPI описывает headers после trusted edge; браузер отправляет только безопасный subset.
  return gatewayHeaders(context) as THeaders;
}
