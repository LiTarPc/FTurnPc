export interface Server {
  id: string;
  name: string;
  host: string;
  password: string;
  deviceId?: string;
  ping?: number;
  icon?: string;
  power?: number;
  provider?: string;
  peer?: string;
  transport?: string;
  obf?: string;
  key?: string;
  cid?: string;
  wg?: string;
  links?: string;
  streamsPerCred?: number;
}

export interface AppSettings {
  bypassMode: 'РУЧ' | 'АВТ';
  power: number;
  mtu: number;
  tray: boolean;
  autoStart: boolean;
  autoConnect: boolean;
  hashes: [string, string, string, string];
  useGlobalHashes: boolean;
  bypassRu: boolean;
  autoUpdateCore: boolean;
  browserKillSwitch: boolean;
  killSwitchMode: 'reconnect' | 'strict';
  disabledBrowsers: string[];
  customBrowsers: string[];
}

export type TunnelState = 'idle' | 'connecting' | 'connected' | 'disconnecting';

export const DEFAULT_SETTINGS: AppSettings = {
  bypassMode: 'АВТ',
  power: 9,
  mtu: 1300,
  tray: true,
  autoStart: true,
  autoConnect: false,
  hashes: ['', '', '', ''],
  useGlobalHashes: false,
  bypassRu: false,
  autoUpdateCore: true,
  browserKillSwitch: false,
  killSwitchMode: 'reconnect',
  disabledBrowsers: [],
  customBrowsers: [],
};
