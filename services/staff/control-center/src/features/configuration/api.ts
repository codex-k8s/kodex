import {
  fetchConfigurationDiff,
  fetchConfigurationSource,
  fetchInstructionHistory,
  fetchInstructionSets,
} from "@/shared/api/adapters/owner-control";
import { fetchConfigurationChanges } from "@/shared/api/adapters/operations";

export const configurationApi = {
  listChanges: fetchConfigurationChanges,
  listInstructions: fetchInstructionSets,
  instructionHistory: fetchInstructionHistory,
  diff: fetchConfigurationDiff,
  source: fetchConfigurationSource,
};
