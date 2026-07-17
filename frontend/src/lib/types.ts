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
}

export type TunnelState = 'idle' | 'connecting' | 'connected' | 'disconnecting';

export interface DeployConfig {
  host: string;
  login: string;
  password: string;
  portsManual: boolean;
  // secrets
  tunnelPassword: string;
  tgAdminId: string;
  tgBotToken: string;
  sshPort: string;
  dtlsPort: string;
  wgPort: string;
}

export const DEFAULT_DEPLOY: DeployConfig = {
  host: '', login: '', password: '', portsManual: false,
  tunnelPassword: '', tgAdminId: '', tgBotToken: '',
  sshPort: '22', dtlsPort: '56000', wgPort: '56001',
};

export type DeployState = 'idle' | 'deploying' | 'removing';

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
};
