import { useState, useEffect } from 'react';
import { IconSettings2, IconX, IconRefresh, IconDownload, IconFolderOpen } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import type { AppSettings } from '../lib/types';
import { SetTrayEnabled, SetAutoStart, GetAutoStart, CheckCoreUpdate, UpdateCore, GetCoreVersion, SelectAndReplaceCore } from '../../wailsjs/go/backend/App';

interface Props {
  onClose: () => void;
}

export default function Settings({ onClose }: Props) {
  const [settings, setSettings] = useState<AppSettings>(() => settingsStore.get());
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);
  const locked = tunnelState === 'connected' || tunnelState === 'connecting';

  // Core Update States
  const [coreVer, setCoreVer] = useState<string>('Загрузка...');
  const [coreUpdate, setCoreUpdate] = useState<any>(null);
  const [coreChecking, setCoreChecking] = useState(false);
  const [coreUpdating, setCoreUpdating] = useState(false);
  const [coreProgress, setCoreProgress] = useState(0);
  const [coreProgressMsg, setCoreProgressMsg] = useState<string>('');

  // Sync autoStart and GetCoreVersion on open
  useEffect(() => {
    GetAutoStart().then((v: any) => {
      if (v !== settings.autoStart) update('autoStart', v);
    });
    GetCoreVersion().then((v: any) => setCoreVer(v || 'Не установлен'));

    const w = window as any;
    if (w.runtime?.EventsOn) {
      w.runtime.EventsOn('core_update_progress', (p: number, msg?: string) => {
        setCoreProgress(p);
        if (msg) setCoreProgressMsg(msg);
      });
      w.runtime.EventsOn('core_update_done', (newVer?: string) => {
        setCoreUpdating(false);
        setCoreProgressMsg('');
        const v = typeof newVer === 'string' && newVer ? newVer : '';
        if (v) {
          setCoreVer(v);
          setCoreUpdate((prev: any) => prev ? { ...prev, hasUpdate: false, currentVersion: v } : null);
        } else {
          GetCoreVersion().then((ver: any) => setCoreVer(ver || 'Установлен'));
          setCoreUpdate((prev: any) => prev ? { ...prev, hasUpdate: false } : null);
        }
      });
    }
  }, []);

  const handleCheckCore = async () => {
    setCoreChecking(true);
    try {
      const res = await CheckCoreUpdate();
      setCoreUpdate(res);
      if (res.currentVersion) setCoreVer(res.currentVersion);
    } catch (e: any) {
      console.error(e);
    } finally {
      setCoreChecking(false);
    }
  };

  const handleDoCoreUpdate = async () => {
    if (!coreUpdate?.downloadUrl && !coreUpdate?.hasUpdate) return;
    setCoreUpdating(true);
    setCoreProgress(5);
    setCoreProgressMsg('Подключение к серверу GitHub...');
    try {
      await UpdateCore(coreUpdate.downloadUrl || '');
    } catch (e: any) {
      alert('Ошибка обновления ядра: ' + e);
      setCoreUpdating(false);
      setCoreProgressMsg('');
    }
  };

  const handleSelectCoreFile = async () => {
    try {
      const newVer = await SelectAndReplaceCore();
      if (newVer) {
        setCoreVer(newVer);
      }
    } catch (e: any) {
      alert('Ошибка при замене ядра: ' + e);
    }
  };

  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => {
    setSettings(s => {
      const next = { ...s, [key]: value };
      settingsStore.save(next);
      return next;
    });
  };

  const filledHashes = settings.hashes.filter(h => h.trim()).length;
  const powerMax = Math.max(9, filledHashes * 27);

  return (
    <>
      <style>{`
        .st-overlay { position: fixed; inset: 0; background: var(--overlay-bg); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .st-modal { background: var(--popup-bg); border-radius: var(--border-radius); padding: 20px; width: 380px; max-width: 95vw; max-height: 90vh; overflow-y: auto; box-shadow: var(--shadow); animation: modal-in 0.3s ease-out; border: 1px solid var(--border); }
        .st-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .st-title { font-size: 15px; font-weight: 600; flex: 1; color: var(--text); }
        .st-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .st-row { display: flex; align-items: center; justify-content: space-between; padding: 11px 0; border-bottom: 1px solid var(--border); font-size: 13px; color: var(--text); }
        .st-row:last-of-type { border-bottom: none; }
        .st-toggle { width: 44px; height: 24px; border-radius: 50px; border: none; cursor: pointer; position: relative; transition: background 0.2s; flex-shrink: 0; }
        .st-toggle--on { background: var(--toggle-on); }
        .st-toggle--off { background: var(--toggle-off); }
        .st-toggle::after { content: ''; position: absolute; width: 16px; height: 16px; border-radius: 50%; top: 4px; transition: left 0.2s; }
        .st-toggle--on::after { background: #ffffff; left: 24px; }
        .st-toggle--off::after { background: var(--text-3); left: 4px; }
        .st-seg { display: flex; background: var(--seg-bg); border-radius: var(--border-radius); padding: 2px; gap: 2px; }
        .st-seg-btn { padding: 5px 13px; border: none; border-radius: calc(var(--border-radius) - 2px); font-size: 12px; font-weight: 600; cursor: pointer; transition: background 0.15s, color 0.15s; background: transparent; color: var(--seg-text); }
        .st-seg-btn--active { background: var(--accent); color: var(--accent-fg); }
        .st-slider-wrap { padding: 4px 0 11px; border-bottom: 1px solid var(--border); }
        .st-slider-label { display: flex; justify-content: space-between; font-size: 13px; color: var(--text); margin-bottom: 8px; }
        .st-slider { width: 100%; -webkit-appearance: none; appearance: none; height: 4px; border-radius: 2px; outline: none; cursor: pointer; background: linear-gradient(to right, var(--accent) calc(var(--v) * 1%), var(--border) calc(var(--v) * 1%)); }
        .st-slider::-webkit-slider-thumb { -webkit-appearance: none; width: 18px; height: 18px; border-radius: 50%; background: var(--primary); border: 2px solid var(--accent); cursor: pointer; }
        .st-hash-btn { width: 100%; margin-top: 16px; padding: 13px; border: 1.5px solid var(--border); border-radius: var(--border-radius); background: var(--button); color: var(--text); font-size: 13px; font-family: var(--font); font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px; }
        .st-locked { opacity: 0.4; pointer-events: none; }
        .st-lock-hint { font-size: 11px; color: var(--text-3); margin-bottom: 4px; text-align: center; }
        .st-nat-box { margin-top: 10px; padding: 10px; background: var(--seg-bg); border-radius: var(--border-radius); font-size: 11px; }
        .st-nat-title { font-weight: 600; color: var(--text); margin-bottom: 4px; display: flex; align-items: center; gap: 6px; }
        .st-nat-sub { color: var(--text-3); font-size: 10px; }
        .st-nat-btn { width: 100%; margin-top: 8px; padding: 6px 12px; background: var(--button); border: 1px solid var(--border); border-radius: var(--border-radius); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 6px; }
      `}</style>
      <div className="st-overlay" onClick={onClose}>
        <div className="st-modal" onClick={e => e.stopPropagation()}>
          <div className="st-header">
            <IconSettings2 stroke={2} size={20} />
            <span className="st-title">Настройки</span>
            <button className="st-close" onClick={onClose}><IconX size={18} /></button>
          </div>

          {locked && <div className="st-lock-hint">Недоступно во время подключения</div>}

          {settings.useGlobalHashes ? (
            <div className={`st-slider-wrap${locked ? ' st-locked' : ''}`}>
              <div className="st-slider-label"><span>Мощность</span><span>{settings.power}</span></div>
              <input
                type="range" min={9} max={powerMax} step={9} value={Math.min(settings.power, powerMax)}
                className="st-slider"
                style={{ '--v': Math.round((Math.min(settings.power, powerMax) - 9) / Math.max(powerMax - 9, 1) * 100) } as React.CSSProperties}
                onChange={e => update('power', +e.target.value)}
              />
            </div>
          ) : (
            <div className="st-slider-wrap" style={{ opacity: 0.5 }}>
              <div className="st-slider-label"><span>Мощность</span><span>профиль</span></div>
              <div style={{ fontSize: 12, color: 'var(--text-3)' }}>Настраивается в редакторе профиля</div>
            </div>
          )}

          <div className="st-row">
            <span>Трей</span>
            <button className={`st-toggle st-toggle--${settings.tray ? 'on' : 'off'}`} onClick={() => {
              const next = !settings.tray;
              update('tray', next);
              SetTrayEnabled(next);
            }} />
          </div>

          <div className="st-row">
            <span>Запускать при старте</span>
            <button className={`st-toggle st-toggle--${settings.autoStart ? 'on' : 'off'}`} onClick={() => {
              const next = !settings.autoStart;
              update('autoStart', next);
              SetAutoStart(next);
            }} />
          </div>

          <div className="st-row">
            <span>Авто-подключение</span>
            <button className={`st-toggle st-toggle--${settings.autoConnect ? 'on' : 'off'}`} onClick={() => update('autoConnect', !settings.autoConnect)} />
          </div>

          <div className="st-row">
            <span>Автопроверка обновлений ядра</span>
            <button className={`st-toggle st-toggle--${settings.autoUpdateCore !== false ? 'on' : 'off'}`} onClick={() => update('autoUpdateCore', settings.autoUpdateCore === false)} />
          </div>

          <div className="st-nat-box" style={{ marginTop: 12 }}>
            <div className="st-nat-title"><IconRefresh size={14} /> Ядро FreeTurn (freeturnclient)</div>
            <div className="st-nat-sub">Статус ядра: <strong>{coreVer}</strong></div>
            {coreUpdate && (
              <div className="st-nat-sub" style={{ color: coreUpdate.hasUpdate ? '#4ade80' : '#94a3b8', marginTop: 2 }}>
                {coreUpdate.hasUpdate ? `Доступна новая версия: ${coreUpdate.latestVersion}` : 'Установлена актуальная версия ядра'}
              </div>
            )}
            {coreUpdating && (
              <div style={{ margin: '8px 0 4px 0', fontSize: '11px', color: '#60a5fa' }}>
                {coreProgressMsg || `Загрузка и установка: ${coreProgress}%`}
                <div style={{ background: '#334155', height: '4px', borderRadius: '2px', overflow: 'hidden', marginTop: '4px' }}>
                  <div style={{ background: '#3b82f6', width: `${coreProgress}%`, height: '100%', transition: 'width 0.2s' }} />
                </div>
              </div>
            )}
            <div style={{ display: 'flex', gap: '8px', marginTop: '8px', flexWrap: 'wrap' }}>
              <button className="st-nat-btn" onClick={handleCheckCore} disabled={coreChecking || coreUpdating}>
                {coreChecking ? 'Проверка...' : 'Проверить обновление'}
              </button>
              {coreUpdate?.hasUpdate && (
                <button className="st-nat-btn" style={{ background: '#166534', color: '#ffffff' }} onClick={handleDoCoreUpdate} disabled={coreUpdating}>
                  <IconDownload size={14} /> {coreUpdating ? 'Обновление...' : 'Обновить ядро'}
                </button>
              )}
              <button className="st-nat-btn" onClick={handleSelectCoreFile} disabled={coreUpdating} title="Выбрать файл freeturnclient вручную с диска">
                <IconFolderOpen size={14} /> Заменить ядро
              </button>
            </div>
          </div>

        </div>
      </div>
    </>
  );
}
