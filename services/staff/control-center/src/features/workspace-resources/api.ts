import {
  commandAccessResource,
  copyGitResource,
  createMutableResource,
  deleteMutableResource,
  detachGitResource,
  fetchResources,
  transitionMutableResource,
  updateMutableResource,
} from "@/shared/api/adapters/resources";
import {
  fetchIntegrationDefinitions,
  fetchRoleDefinitions,
} from "@/shared/api/adapters/owner-control";

export const workspaceResourcesApi = {
  list: fetchResources,
  listIntegrationDefinitions: fetchIntegrationDefinitions,
  listRoleDefinitions: fetchRoleDefinitions,
  createMutable: createMutableResource,
  updateMutable: updateMutableResource,
  transitionMutable: transitionMutableResource,
  deleteMutable: deleteMutableResource,
  manageAccess: commandAccessResource,
  detachAccess: detachGitResource,
  copyAccess: copyGitResource,
};
