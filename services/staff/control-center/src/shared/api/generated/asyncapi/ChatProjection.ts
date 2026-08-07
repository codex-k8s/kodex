import {ChatRoomType} from './ChatRoomType';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface ChatProjection {
  stableKey: string;
  roomType: ChatRoomType;
  defaultAgentId?: string;
  externalChannelRef: string;
  workPolicy: string;
  ownership: ConfigurationOwnershipProjection;
}
export { ChatProjection };