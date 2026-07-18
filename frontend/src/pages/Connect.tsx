import { useState, useEffect, useRef } from 'react';
import type React from 'react';
import {
  IconPlus, IconSettings, IconTrash, IconChevronUp,
  IconCloverFilled, IconFlameFilled, IconShieldFilled, IconLayoutGridFilled, IconCloudFilled, IconBrandSpeedtest,
  IconStarFilled, IconHeartFilled, IconBoltFilled, IconRocket,
  IconCrownFilled, IconDiamondFilled, IconLeafFilled, IconSnowflake,
  IconServer, IconGlobe, IconLockFilled, IconWifi, IconPlugConnected,
} from '@tabler/icons-react';
import { EventsOn } from '../../wailsjs/runtime/runtime';

const SERVER_ICONS: { key: string; render: (size: number) => React.ReactNode }[] = [
  { key: 'clover',     render: s => <IconCloverFilled size={s} /> },
  { key: 'flame',      render: s => <IconFlameFilled size={s} /> },
  { key: 'shield',     render: s => <IconShieldFilled size={s} /> },
  { key: 'grid',       render: s => <IconLayoutGridFilled size={s} /> },
  { key: 'cloud',      render: s => <IconCloudFilled size={s} /> },
  { key: 'speed',      render: s => <IconBrandSpeedtest size={s} stroke={2} /> },
  { key: 'star',       render: s => <IconStarFilled size={s} /> },
  { key: 'heart',      render: s => <IconHeartFilled size={s} /> },
  { key: 'bolt',       render: s => <IconBoltFilled size={s} /> },
  { key: 'rocket',     render: s => <IconRocket size={s} stroke={2} /> },
  { key: 'crown',      render: s => <IconCrownFilled size={s} /> },
  { key: 'diamond',    render: s => <IconDiamondFilled size={s} /> },
  { key: 'leaf',       render: s => <IconLeafFilled size={s} /> },
  { key: 'snowflake',  render: s => <IconSnowflake size={s} stroke={2} /> },
  { key: 'server',     render: s => <IconServer size={s} stroke={2} /> },
  { key: 'globe',      render: s => <IconGlobe size={s} stroke={2} /> },
  { key: 'lock',       render: s => <IconLockFilled size={s} /> },
  { key: 'wifi',       render: s => <IconWifi size={s} stroke={2} /> },
  { key: 'flag-ru',    render: () => <img src="/flags/ru.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-us',    render: () => <img src="/flags/us.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-de',    render: () => <img src="/flags/de.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-nl',    render: () => <img src="/flags/nl.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-fi',    render: () => <img src="/flags/fi.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-fr',    render: () => <img src="/flags/fr.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-gb',    render: () => <img src="/flags/gb.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-jp',    render: () => <img src="/flags/jp.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-pl',    render: () => <img src="/flags/pl.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-se',    render: () => <img src="/flags/se.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ch',    render: () => <img src="/flags/ch.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-lt',    render: () => <img src="/flags/lt.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-lv',    render: () => <img src="/flags/lv.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ee',    render: () => <img src="/flags/ee.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-cz',    render: () => <img src="/flags/cz.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-at',    render: () => <img src="/flags/at.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ca',    render: () => <img src="/flags/ca.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-au',    render: () => <img src="/flags/au.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-sg',    render: () => <img src="/flags/sg.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-hk',    render: () => <img src="/flags/hk.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-tr',    render: () => <img src="/flags/tr.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-kz',    render: () => <img src="/flags/kz.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
];

function ServerIcon({ iconKey, size }: { iconKey?: string; size: number }) {
  const entry = SERVER_ICONS.find(i => i.key === (iconKey ?? 'clover')) ?? SERVER_ICONS[0];
  return (
    <div style={{
      width: size,
      height: size,
      borderRadius: '5px',
      overflow: 'hidden',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      flexShrink: 0,
    }}>
      {entry.render(size)}
    </div>
  );
}
import AddServer from '../modals/Add-server';
import { ViewServer } from '../modals/View-server';
import { serverStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { settingsStore } from '../lib/store';
import { themeStore } from '../lib/stores/themeStore';
import { toastStore } from '../lib/stores/toastStore';
import { logStore } from '../lib/stores/logStore';
import { wdttLinkStore } from '../lib/utils/wdttLink';
import { SaveProfile } from '../../wailsjs/go/backend/App';
import type { Server, TunnelState } from '../lib/types';
import { Connect as WailsConnect, Disconnect as WailsDisconnect, ListProfiles, DeleteProfile } from '../../wailsjs/go/backend/App';
import shapeLight from '../assets/shape-light.png';
import shapeDark from '../assets/shape-dark.png';
import powerIcon from '../assets/power-icon.png';

const PING_COLORS: Record<string, string> = {
  good: '#22c55e',
  mid: '#f59e0b',
  bad: '#ef4444',
  none: 'var(--border)',
};

function pingColor(ping?: number) {
  if (!ping) return PING_COLORS.none;
  if (ping < 100) return PING_COLORS.good;
  if (ping < 200) return PING_COLORS.mid;
  return PING_COLORS.bad;
}

const TUNNEL_LABEL: Record<TunnelState, string> = {
  idle: 'Подключить',
  connecting: 'Подключение...',
  connected: 'Отключить',
  disconnecting: 'Отключение...',
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec === 0) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  const i = Math.floor(Math.log(bytesPerSec) / Math.log(k));
  return parseFloat((bytesPerSec / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export default function Connect() {
  const [servers, setServers] = useState<Server[]>(() => serverStore.getAll());
  const [selected, setSelected] = useState<Server | null>(() => {
    const all = serverStore.getAll();
    if (all.length === 0) return null;
    const lastId = serverStore.getLastSelectedId();
    return all.find(s => s.id === lastId) ?? all[0];
  });
  const [listOpen, setListOpen] = useState(false);

  const [stats, setStats] = useState<{ rx: number; tx: number; downSpeed: number; upSpeed: number } | null>(null);
  const prevStatsRef = useRef<{ rx: number; tx: number; time: number } | null>(null);

  useEffect(() => {
    const handleStats = (data: any) => {
      if (!data) return;
      const rx = data.rx || 0;
      const tx = data.tx || 0;
      const now = Date.now();
      
      if (prevStatsRef.current) {
        const timeDiff = (now - prevStatsRef.current.time) / 1000;
        if (timeDiff > 0) {
          const downSpeed = Math.max(0, (rx - prevStatsRef.current.rx) / timeDiff);
          const upSpeed = Math.max(0, (tx - prevStatsRef.current.tx) / timeDiff);
          setStats({ rx, tx, downSpeed, upSpeed });
        }
      } else {
        setStats({ rx, tx, downSpeed: 0, upSpeed: 0 });
      }
      
      prevStatsRef.current = { rx, tx, time: now };
    };

    EventsOn('stats', handleStats);
  }, []);

  useEffect(() => {
    ListProfiles().then((profiles: any) => {
      if (!profiles) return;
      const existing = serverStore.getAll();
      let changed = false;
      
      const updatedServers = existing
        .filter(s => !!profiles[s.name])
        .map(s => {
          const bp = profiles[s.name];
          const merged = {
            ...s,
            host: bp.peer || s.host,
            password: bp.key || s.password,
            provider: bp.provider || s.provider,
            peer: bp.peer || s.peer,
            transport: bp.transport || s.transport,
            obf: bp.obf || s.obf,
            key: bp.key || s.key,
            cid: bp.cid || s.cid,
            wg: bp.wg || s.wg,
            links: bp.links || s.links,
            power: bp.power ?? s.power,
            streamsPerCred: bp.streamsPerCred ?? s.streamsPerCred,
          };
          if (JSON.stringify(s) !== JSON.stringify(merged)) {
            changed = true;
          }
          return merged;
        });

      if (existing.length !== updatedServers.length) {
        changed = true;
      }

      // Add profiles that are on disk but not in localStorage
      for (const [name, p] of Object.entries(profiles as any)) {
        if (!existing.some(s => s.name === name)) {
          const bp = p as any;
          const host = bp.peer || '';
          if (!host) continue;
          updatedServers.push({
            id: crypto.randomUUID(),
            name,
            host,
            password: bp.key || '',
            provider: bp.provider || '',
            peer: bp.peer || '',
            transport: bp.transport || '',
            obf: bp.obf || '',
            key: bp.key || '',
            cid: bp.cid || '',
            wg: bp.wg || '',
            links: bp.links || '',
            power: bp.power,
            streamsPerCred: bp.streamsPerCred,
          });
          changed = true;
        }
      }

      if (changed) {
        serverStore.save(updatedServers);
        setServers(updatedServers);
      }
      
      const currentList = changed ? updatedServers : existing;
      if (currentList.length > 0) {
        const lastId = serverStore.getLastSelectedId();
        const currentSelected = currentList.find(s => s.id === lastId) ?? currentList[0];
        if (!selectedRef.current || !currentList.some(s => s.id === selectedRef.current?.id)) {
          setSelected(currentSelected || null);
        }
      } else {
        setSelected(null);
      }
    }).catch(console.error);
  }, []);

  const [tunnelState, setTunnelState] = useState<TunnelState>(() => tunnelStore.get());
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  useEffect(() => {
    if (tunnelState !== 'connected') {
      setStats(null);
      prevStatsRef.current = null;
    }
  }, [tunnelState]);

  const selectedRef = useRef(selected);
  selectedRef.current = selected;

  const tunnelStateRef = useRef(tunnelState);
  tunnelStateRef.current = tunnelState;

  useEffect(() => {
    serverStore.setLastSelectedId(selected?.id ?? null);
  }, [selected?.id]);

  useEffect(() => {
    const s = settingsStore.get();
    if (!s.autoConnect) return;
    if (tunnelStateRef.current !== 'idle') return;
    if (!selectedRef.current) return;
    doConnect();
  }, []);

  const [addServerOpen, setAddServerOpen] = useState(false);
  const [viewServer, setViewServer] = useState<Server | null>(null);
  const [theme, setTheme] = useState(() => themeStore.get());
  useEffect(() => themeStore.subscribe(setTheme), []);

  const [linkFlash, setLinkFlash] = useState(false);

  useEffect(() => {
    return wdttLinkStore.subscribe((link) => {
      if (!link) return;
      const consumed = wdttLinkStore.consume();
      if (!consumed) return;
      const name = consumed.name;

      const applyLink = async () => {
        const fullProfile = {
          name: name,
          provider: consumed.provider || '',
          peer: consumed.peer || '',
          transport: consumed.transport || '',
          obf: consumed.obf || '',
          key: consumed.key || '',
          cid: consumed.cid || '',
          wg: consumed.wg || '',
          links: consumed.links || '',
        };
        await SaveProfile(name, fullProfile as any);
        const existing = serverStore.getAll().find(s => s.name === name);
        let s;
        if (existing) {
          s = {
            ...existing,
            ...fullProfile,
            host: fullProfile.peer || existing.host,
            password: fullProfile.key || existing.password,
          };
          serverStore.update(s);
        } else {
          s = serverStore.add({
            ...fullProfile,
            host: fullProfile.peer || '',
            password: fullProfile.key || '',
          });
        }
        setServers(serverStore.getAll());
        setSelected({ ...s });
        setLinkFlash(true);
        setTimeout(() => setLinkFlash(false), 800);
        toastStore.show(existing ? `Профиль обновлён: ${name}` : `Профиль добавлен: ${name}`, 3000);
      };
      applyLink();
    });
  }, []);

  const doConnect = async () => {
    const cur = selectedRef.current;
    if (!cur) return;
    
    tunnelStore.set('connecting');
    logStore.clear();
    logStore.push('INFO', `Подключение к профилю: ${cur.name}`);
    try {
      const workers = cur.power || 10;
      const bypassRu = settingsStore.get().bypassRu;
      await WailsConnect({
        profile: cur.name,
        workers,
        bypassRu,
      });
      logStore.push('INFO', 'WailsConnect вернул OK (процесс запущен)');
    } catch (e: any) {
      const msg = e?.message || String(e);
      logStore.push('ERROR', `Ошибка Connect: ${msg}`);
      toastStore.show(`Ошибка: ${msg}`, 5000);
      tunnelStore.set('idle');
    }
  };

  const [reconnectAt, setReconnectAt] = useState(0); // timestamp когда можно снова подключиться

  const handleTunnel = async () => {
    if (!selectedRef.current) return;
    if (tunnelState === 'idle') {
      if (Date.now() < reconnectAt) {
        const secs = Math.ceil((reconnectAt - Date.now()) / 1000);
        toastStore.show(`Подождите ${secs} сек.`, 2000);
        return;
      }
      await doConnect();
    } else if (tunnelState === 'connected' || tunnelState === 'connecting') {
      tunnelStore.set('disconnecting');
      await WailsDisconnect();
      tunnelStore.set('idle');
      setReconnectAt(Date.now() + 4000);
    }
  };

  const handleAdd = (data: Omit<Server, 'id'>) => {
    const s = serverStore.add(data);
    setServers(serverStore.getAll());
    setSelected(s);
  };

  const handleDelete = async (id: string) => {
    const target = serverStore.getAll().find(s => s.id === id);
    if (target) {
      try {
        await DeleteProfile(target.name);
      } catch (e) {
        console.error("Failed to delete profile from disk:", e);
      }
    }
    serverStore.remove(id);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === id) setSelected(all[0] ?? null);
  };

  const [iconMenu, setIconMenu] = useState<{ server: Server; x: number; y: number } | null>(null);

  const handleIconClick = (e: React.MouseEvent, server: Server) => {
    e.stopPropagation();
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    setIconMenu({ server, x: rect.left, y: rect.top });
  };

  const handlePickIcon = (key: string) => {
    if (!iconMenu) return;
    const updated = { ...iconMenu.server, icon: key };
    serverStore.update(updated);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === iconMenu.server.id) setSelected(updated);
    setIconMenu(null);
  };

  const isActive = tunnelState === 'connected';
  const isSpinning = tunnelState === 'connecting' || tunnelState === 'disconnecting';
  const isBusy = tunnelState === 'disconnecting';

  return (
    <>
      <style>{`
        * { font-family: 'Geist', sans-serif; font-weight: 500; box-sizing: border-box; }
        .main {
          flex: 1;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: space-between;
          padding: 16px 20px 24px 20px;
          animation: page-in 0.25s ease-out;
          background: var(--bg);
          overflow: hidden;
          height: 100%;
        }
        .header-bar {
          width: 100%;
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 0 4px;
        }
        .brand-title {
          font-size: 18px;
          font-weight: 700;
          color: var(--text);
          display: flex;
          align-items: center;
          gap: 8px;
        }
        .btn-add {
          background: none;
          border: none;
          cursor: pointer;
          color: var(--text);
          padding: 6px;
          border-radius: 8px;
          display: flex;
          align-items: center;
          justify-content: center;
          transition: background 0.15s;
        }
        .btn-add:hover {
          background: rgba(255, 255, 255, 0.15);
        }
        .center-area {
          flex: 1;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          width: 100%;
          gap: 20px;
        }
        .power-btn {
          position: relative;
          width: 160px;
          height: 160px;
          background: none;
          border: none;
          cursor: pointer;
          display: flex;
          align-items: center;
          justify-content: center;
          padding: 0;
          transition: opacity 0.2s;
        }
        .power-btn:disabled {
          opacity: 0.5;
          cursor: not-allowed;
        }
        .orb {
          position: absolute;
          width: 130px;
          height: 130px;
        }
        .orb img {
          width: 100%;
          height: 100%;
          display: block;
        }
        .orb--spinning {
          animation: shape-spin 2s linear infinite;
        }
        .orb--active {
          animation: shape-pulse 1.2s ease-in-out infinite;
        }
        @keyframes shape-spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
        @keyframes shape-pulse {
          0%, 100% { transform: scale(1); }
          50% { transform: scale(1.08); }
        }
        @keyframes link-flash {
          0% { opacity: 1; }
          30% { opacity: 0.2; }
          60% { opacity: 1; }
          80% { opacity: 0.4; }
          100% { opacity: 1; }
        }
        .orb--flash {
          animation: link-flash 0.8s ease-out;
        }
        .power-icon {
          position: relative;
          z-index: 1;
          display: flex;
          align-items: center;
          justify-content: center;
        }
        .tunnel-label {
          font-size: 14px;
          color: var(--text-2);
          font-weight: 600;
          text-align: center;
        }
        .stats-card {
          display: flex;
          align-items: center;
          justify-content: space-between;
          width: 100%;
          max-width: 320px;
          background: var(--surface-glass);
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          border: 1px solid var(--border-glass);
          border-radius: 16px;
          padding: 12px 16px;
          box-shadow: var(--shadow);
          animation: slide-down 0.2s ease-out;
          height: 76px;
        }
        .stats-col {
          flex: 1;
          display: flex;
          flex-direction: column;
          align-items: center;
        }
        .stats-divider {
          width: 1px;
          height: 36px;
          background: var(--border-glass);
          margin: 0 12px;
        }
        .stats-speed {
          font-size: 14px;
          font-weight: 700;
          color: var(--text);
          margin-bottom: 2px;
          white-space: nowrap;
        }
        .stats-label {
          font-size: 10px;
          text-transform: uppercase;
          letter-spacing: 0.5px;
          color: var(--text-3);
          margin-bottom: 1px;
          white-space: nowrap;
        }
        .stats-value {
          font-size: 11px;
          font-weight: 600;
          color: var(--text-2);
          white-space: nowrap;
        }
        .status-bar {
          width: 100%;
          max-width: 320px;
          display: flex;
          flex-direction: column;
          align-items: stretch;
          z-index: 10;
        }
        .server-list {
          border: 1px solid var(--border-glass);
          border-radius: 12px;
          overflow-y: auto;
          max-height: 180px;
          margin-bottom: 8px;
          background: var(--surface-glass);
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          box-shadow: var(--shadow);
          animation: slide-down 0.28s ease-out;
        }
        .server-item {
          display: flex;
          align-items: center;
          gap: 10px;
          width: 100%;
          padding: 10px 16px;
          background: transparent;
          font-size: 14px;
          color: var(--text);
          font-family: 'Geist', sans-serif;
          font-weight: 500;
          border-bottom: 1px solid var(--border-glass);
          border-top: none;
          border-left: none;
          border-right: none;
        }
        .server-item:last-child {
          border-bottom: none;
        }
        .server-item:hover {
          background: rgba(255, 255, 255, 0.15);
        }
        .server-item--active {
          background: rgba(255, 255, 255, 0.25);
        }
        .server-icon-btn {
          background: none;
          border: none;
          cursor: pointer;
          padding: 0;
          display: flex;
          align-items: center;
          color: var(--text);
        }
        .server-edit-btn {
          background: none;
          border: none;
          cursor: pointer;
          padding: 4px;
          display: flex;
          align-items: center;
          color: var(--text-3);
          opacity: 0.6;
          transition: opacity 0.15s, color 0.15s;
        }
        .server-edit-btn:hover {
          opacity: 1;
          color: var(--text);
        }
        .status-server {
          display: flex;
          align-items: center;
          gap: 10px;
          background: var(--surface-glass);
          backdrop-filter: blur(12px);
          -webkit-backdrop-filter: blur(12px);
          border: 1px solid var(--border-glass);
          border-radius: 12px;
          padding: 10px 16px;
          font-size: 14px;
          color: var(--text);
          cursor: pointer;
          width: 100%;
          font-family: 'Geist', sans-serif;
          font-weight: 600;
          box-shadow: var(--shadow);
          transition: background 0.2s;
        }
        .status-server:hover {
          background: rgba(255, 255, 255, 0.55);
        }
        .status-server--empty {
          color: var(--text-4);
        }
        .status-name {
          flex: 1;
          text-align: left;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
        .status-ping {
          display: flex;
          align-items: center;
          gap: 6px;
          font-size: 13px;
        }
        .ping-dot {
          width: 8px;
          height: 8px;
          border-radius: 50%;
        }
        .icon-picker {
          position: fixed;
          z-index: 200;
          background: var(--surface-glass);
          backdrop-filter: blur(16px);
          -webkit-backdrop-filter: blur(16px);
          border: 1px solid var(--border-glass);
          border-radius: 12px;
          padding: 10px;
          box-shadow: var(--shadow);
          display: grid;
          grid-template-columns: repeat(6, 36px);
          gap: 4px;
          animation: modal-in 0.15s ease-out;
        }
        .icon-picker-btn {
          width: 36px;
          height: 36px;
          display: flex;
          align-items: center;
          justify-content: center;
          background: none;
          border: 1px solid transparent;
          border-radius: 8px;
          cursor: pointer;
          color: var(--text);
          font-size: 18px;
        }
        .icon-picker-btn:hover {
          background: rgba(255, 255, 255, 0.2);
          border-color: var(--border-glass);
        }
        .icon-picker-btn--active {
          background: rgba(255, 255, 255, 0.3);
          border-color: var(--accent);
        }
        @media (max-height: 550px) {
          .power-btn {
            width: 120px;
            height: 120px;
          }
          .orb {
            width: 96px;
            height: 96px;
          }
          .center-area {
            gap: 12px;
          }
        }
      `}</style>
      <main className="main">
        <div className="header-bar">
          <div className="brand-title">
            <IconPlugConnected size={20} stroke={2.5} style={{ color: 'var(--accent)' }} />
            <span>FreeTurn</span>
          </div>
          <button className="btn-add" onClick={() => setAddServerOpen(true)}>
            <IconPlus stroke={2} size={22} />
          </button>
        </div>

        <div className="center-area">
          <button
            className="power-btn"
            onClick={handleTunnel}
            disabled={!selected || isBusy}
            title={selected ? TUNNEL_LABEL[tunnelState] : 'Добавьте сервер'}
          >
            <div className={`orb${isSpinning ? ' orb--spinning' : isActive ? ' orb--active' : ''}${linkFlash ? ' orb--flash' : ''}`}>
              <img src={theme === 'dark' ? shapeDark : shapeLight} alt="" draggable={false} />
            </div>
            <div className="power-icon">
              <img src={powerIcon} alt="" draggable={false} style={{ width: 28, height: 35 }} />
            </div>
          </button>

          <span className="tunnel-label">{selected ? TUNNEL_LABEL[tunnelState] : 'Нет серверов'}</span>

          {isActive && stats && (
            <div className="stats-card">
              <div className="stats-col">
                <span className="stats-speed">{formatSpeed(stats.downSpeed)} ↓</span>
                <span className="stats-label">Скачано</span>
                <span className="stats-value">{formatBytes(stats.rx)}</span>
              </div>
              <div className="stats-divider" />
              <div className="stats-col">
                <span className="stats-speed">{formatSpeed(stats.upSpeed)} ↑</span>
                <span className="stats-label">Отправлено</span>
                <span className="stats-value">{formatBytes(stats.tx)}</span>
              </div>
            </div>
          )}
        </div>

        <div className="status-bar">
          {listOpen && servers.length > 0 && (
            <div className="server-list">
              {servers.map(s => (
                <div
                  key={s.id}
                  className={`server-item${s.id === selected?.id ? ' server-item--active' : ''}`}
                  style={{ cursor: 'pointer' }}
                  onClick={() => { setSelected({ ...s }); setListOpen(false); }}
                >
                  <button className="server-icon-btn" onClick={(e) => { e.stopPropagation(); handleIconClick(e, s); }}>
                    <ServerIcon iconKey={s.icon} size={20} />
                  </button>
                  <span className="status-name">
                    {s.name}
                  </span>
                  {s.ping != null && (
                    <span className="status-ping">
                      <span className="ping-dot" style={{ background: pingColor(s.ping) }} />
                      {s.ping}
                    </span>
                  )}
                  <button className="server-edit-btn" onClick={(e) => { e.stopPropagation(); setViewServer(s); }} title="Просмотр профиля">
                    <IconSettings size={15} stroke={2} />
                  </button>
                  <button className="server-edit-btn" onClick={(e) => { e.stopPropagation(); handleDelete(s.id); }} title="Удалить">
                    <IconTrash size={15} stroke={2} />
                  </button>
                </div>
              ))}
            </div>
          )}

          <button className={`status-server${!selected ? ' status-server--empty' : ''}`} onClick={() => setListOpen(o => !o)}>
            <ServerIcon iconKey={selected?.icon} size={20} />
            <span className="status-name">{selected ? selected.name : 'Нет серверов'}</span>
            {selected?.ping != null && (
              <span className="status-ping">
                <span className="ping-dot" style={{ background: pingColor(selected.ping) }} />
                {selected.ping}
              </span>
            )}
            <IconChevronUp
              size={16}
              style={{ transform: listOpen ? 'rotate(0deg)' : 'rotate(180deg)', transition: 'transform 0.2s' }}
            />
          </button>
        </div>

        {addServerOpen && <AddServer onClose={() => setAddServerOpen(false)} onAdd={handleAdd} />}
        {viewServer && (
          <ViewServer 
            server={viewServer} 
            onClose={() => setViewServer(null)} 
            onSave={(updated) => {
              const all = serverStore.getAll();
              setServers(all);
              if (selected?.id === updated.id) {
                setSelected({ ...updated });
              }
            }}
          />
        )}

        {iconMenu && (
          <>
            <div style={{ position: 'fixed', inset: 0, zIndex: 199 }} onClick={() => setIconMenu(null)} />
            <div
              className="icon-picker"
              style={{
                left: Math.min(iconMenu.x, window.innerWidth - 256),
                top: iconMenu.y - 4 - (Math.ceil(SERVER_ICONS.length / 6) * 40 + 20),
              }}
            >
              {SERVER_ICONS.map(ic => (
                <button
                  key={ic.key}
                  className={`icon-picker-btn${(iconMenu.server.icon ?? 'clover') === ic.key ? ' icon-picker-btn--active' : ''}`}
                  onClick={() => handlePickIcon(ic.key)}
                  title={ic.key}
                >
                  {ic.render(18)}
                </button>
              ))}
            </div>
          </>
        )}
      </main>

    </>
  );
}
