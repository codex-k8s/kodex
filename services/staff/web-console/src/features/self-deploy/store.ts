import { defineStore } from 'pinia';

import { canQueryAgentScope, fetchSelfDeploySummary, sendSelfDeployGateDecision } from '@/shared/api/staff-gateway';
import type { OperatorContext } from '@/shared/api/context';
import type { ApiError } from '@/shared/api/errors';
import type { SelfDeployGateDecisionAction, SelfDeployGateDecisionSummary, SelfDeploySummary } from '@/shared/api/generated';

export const useSelfDeployStore = defineStore('self-deploy', {
  state: () => ({
    summary: undefined as SelfDeploySummary | undefined,
    isLoading: false,
    isSubmittingDecision: false,
    unsupportedAgentScope: false,
    error: undefined as ApiError | undefined,
    decisionError: undefined as ApiError | undefined,
    lastDecision: undefined as SelfDeployGateDecisionSummary | undefined,
  }),
  getters: {
    isReady: (state) => state.summary?.availability === 'ready',
    isUnavailable: (state) => state.summary?.availability === 'unavailable',
  },
  actions: {
    async load(context: OperatorContext) {
      if (!canQueryAgentScope(context)) {
        this.summary = undefined;
        this.unsupportedAgentScope = true;
        this.error = undefined;
        return;
      }
      this.unsupportedAgentScope = false;
      this.isLoading = true;
      this.error = undefined;
      this.decisionError = undefined;
      try {
        const response = await fetchSelfDeploySummary(context);
        this.summary = response.summary;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoading = false;
      }
    },
    async submitDecision(context: OperatorContext, action: SelfDeployGateDecisionAction, comment?: string) {
      const gate = this.summary?.governance;
      if (!this.summary?.self_deploy_plan_id || !gate?.gate_request_id || !gate.gate_request_version) {
        return;
      }
      this.isSubmittingDecision = true;
      this.decisionError = undefined;
      try {
        const response = await sendSelfDeployGateDecision(context, gate.gate_request_id, {
          self_deploy_plan_ref: this.summary.self_deploy_plan_id,
          action,
          comment,
          idempotency_key: `web-console:self-deploy-gate:${gate.gate_request_id}:${gate.gate_request_version}:${action}`,
          expected_version: gate.gate_request_version,
          expected_status: 'pending',
          decision_policy_ref: gate.gate_policy_ref,
        });
        this.lastDecision = response.decision;
        await this.load(context);
      } catch (error) {
        this.decisionError = error as ApiError;
      } finally {
        this.isSubmittingDecision = false;
      }
    },
  },
});
