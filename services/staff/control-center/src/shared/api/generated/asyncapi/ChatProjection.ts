import {ChatRoomType} from './ChatRoomType';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface ChatProjection {
  stableKey: string;
  roomType: ChatRoomType;
  defaultAgentStatus: string;
  providerChannelStatus: string;
  workPolicy: string;
  ownership: ConfigurationOwnershipProjection;
}
export { ChatProjection };