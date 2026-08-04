import { useState, useEffect } from 'react';
import { IconSettings2, IconChevronDown, IconX, IconAlertTriangle, IconActivity, IconRefresh, IconDownload } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import type { AppSettings } from '../lib/types';
import { SetTrayEnabled, SetAutoStart, GetAutoStart, CheckNAT, CheckCoreUpdate, UpdateCore, GetCoreVersion } from '../../wailsjs/go/backend/App';

interface Props {
  onClose: () => void;
}

export default function Settings({ onClose }: Props) {
  const [settings, setSettings] = useState<AppSettings>(() => settingsStore.get());
  const [mtuRaw, setMtuRaw] = useState(String(settingsStore.get().mtu ?? 1300));
  const mtuValid = (() => { const n = Number(mtuRaw); return Number.isInteger(n) && n >= 576 && n <= 1500; })();
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);
  const locked = tunnelState === 'connected' || tunnelState === 'connecting';
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [advancedConfirm, setAdvancedConfirm] = useState(false);
  const [natResult, setNatResult] = useState<any>(null);
  const [natLoading, setNatLoading] = useState(false);

  // Core Update States
  const [coreVer, setCoreVer] = useState<string>('Загрузка...');
  const [coreUpdate, setCoreUpdate] = useState<any>(null);
  const [coreChecking, setCoreChecking] = useState(false);
  const [coreUpdating, setCoreUpdating] = useState(false);
  const [coreProgress, setCoreProgress] = useState(0);

  // Sync autoStart and GetCoreVersion on open
  useEffect(() => {
    GetAutoStart().then((v: any) => {
      if (v !== settings.autoStart) update('autoStart', v);
    });
    GetCoreVersion().then((v: any) => setCoreVer(v || 'Не установлен'));

    const w = window as any;
    if (w.runtime?.EventsOn) {
      w.runtime.EventsOn('core_update_progress', (p: number) => setCoreProgress(p));
      w.runtime.EventsOn('core_update_done', () => {
        setCoreUpdating(false);
        GetCoreVersion().then((v: any) => setCoreVer(v || 'Установлен'));
        setCoreUpdate(null);
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
    try {
      await UpdateCore(coreUpdate.downloadUrl || '');
    } catch (e: any) {
      alert('Ошибка обновления ядра: ' + e);
      setCoreUpdating(false);
    }
  };

  const update = <K extends keyof AppSettings>(key: K, value: AppSettings[K]) => {
    setSettings(s => {
      const next = { ...s, [key]: value };
      settingsStore.save(next);
      return next;
    });
  };

  const handleClose = () => {
    const n = Number(mtuRaw);
    const mtu = mtuValid ? n : settings.mtu;
    if (mtu !== settings.mtu) {
      update('mtu', mtu);
    }
    onClose();
  };

  const handleCheckNAT = async () => {
    setNatLoading(true);
    try {
      const res = await CheckNAT();
      setNatResult(res);
    } catch (e: any) {
      setNatResult({ natType: 'Ошибка', details: e?.message || String(e) });
    } finally {
      setNatLoading(false);
    }
  };

  const filledHashes = settings.hashes.filter(h => h.trim()).length;
  const powerMax = Math.max(9, filledHashes * 27);

  return (
    <>
      <style>{`
        .st-overlay { position: fixed; inset: 0; background: var(--overlay-bg); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .st-modal { background: var(--popup-bg); border-radius: var(--border-radius); padding: 20px; width: 380px; max-width: 95vw; box-shadow: var(--shadow); animation: modal-in 0.3s ease-out; border: 1px solid var(--border); }
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
        .st-num-input { width: 80px; padding: 5px 10px; border: 1.5px solid var(--input-border); border-radius: var(--border-radius); font-size: 13px; font-family: var(--font); text-align: right; outline: none; background: var(--input-bg); color: var(--text); transition: border-color 0.15s; }
        .st-num-input:focus { border-color: var(--input-focus); }
        .st-num-input--error { border-color: #ef4444; }
        .st-hash-btn { width: 100%; margin-top: 16px; padding: 13px; border: 1.5px solid var(--border); border-radius: var(--border-radius); background: var(--button); color: var(--text); font-size: 13px; font-family: var(--font); font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 8px; }
        .st-locked { opacity: 0.4; pointer-events: none; }
        .st-lock-hint { font-size: 11px; color: var(--text-3); margin-bottom: 4px; text-align: center; }
        .st-adv-toggle { width: 100%; display: flex; align-items: center; justify-content: space-between; background: none; border: none; border-top: 1px solid var(--border); padding: 11px 0 0; cursor: pointer; font-size: 12px; font-weight: 600; color: var(--text-3); font-family: var(--font); margin-top: 4px; }
        .st-adv-toggle svg { transition: transform 0.2s; }
        .st-adv-toggle--open svg { transform: rotate(180deg); }
        .st-adv-body { overflow: hidden; transition: max-height 0.25s ease, opacity 0.2s; }
        .st-adv-body--open { max-height: 380px; opacity: 1; }
        .st-adv-body--closed { max-height: 0; opacity: 0; pointer-events: none; }
        .st-confirm-overlay { position: fixed; inset: 0; background: var(--overlay-bg); display: flex; align-items: center; justify-content: center; z-index: 200; }
        .st-confirm { background: var(--popup-bg); border-radius: var(--border-radius); padding: 22px 20px 18px; width: 320px; max-width: 92vw; box-shadow: var(--shadow); border: 1px solid var(--border); }
        .st-confirm-title { font-size: 14px; font-weight: 700; color: var(--text); margin-bottom: 8px; display: flex; align-items: center; gap: 6px; }
        .st-confirm-text { font-size: 12px; color: var(--text-2); line-height: 1.5; margin-bottom: 18px; }
        .st-confirm-actions { display: flex; gap: 8px; justify-content: flex-end; }
        .st-confirm-btn { padding: 8px 18px; border-radius: var(--border-radius); border: none; font-size: 12px; font-family: var(--font); font-weight: 600; cursor: pointer; }
        .st-confirm-btn--cancel { background: var(--seg-bg); color: var(--text); }
        .st-confirm-btn--ok { background: var(--accent); color: var(--accent-fg); }
        .st-nat-box { margin-top: 10px; padding: 10px; background: var(--seg-bg); border-radius: var(--border-radius); font-size: 11px; }
        .st-nat-title { font-weight: 600; color: var(--text); margin-bottom: 4px; display: flex; align-items: center; gap: 6px; }
        .st-nat-val { color: var(--accent); font-weight: 700; margin-bottom: 2px; }
        .st-nat-sub { color: var(--text-3); font-size: 10px; }
        .st-nat-btn { width: 100%; margin-top: 8px; padding: 6px 12px; background: var(--button); border: 1px solid var(--border); border-radius: var(--border-radius); color: var(--text); font-size: 12px; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 6px; }
      `}</style>
      <div className="st-overlay" onClick={handleClose}>
        <div className="st-modal" onClick={e => e.stopPropagation()}>
          <div className="st-header">
            <IconSettings2 stroke={2} size={20} />
            <span className="st-title">Настройки</span>
            <button className="st-close" onClick={handleClose}><IconX size={18} /></button>
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
            <span>Обход RU-ресурсов</span>
            <button className={`st-toggle st-toggle--${settings.bypassRu ? 'on' : 'off'}`} onClick={() => update('bypassRu', !settings.bypassRu)} />
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
                Загрузка и установка: {coreProgress}%
                <div style={{ background: '#334155', height: '4px', borderRadius: '2px', overflow: 'hidden', marginTop: '4px' }}>
                  <div style={{ background: '#3b82f6', width: `${coreProgress}%`, height: '100%', transition: 'width 0.2s' }} />
                </div>
              </div>
            )}
            <div style={{ display: 'flex', gap: '8px', marginTop: '8px' }}>
              <button className="st-nat-btn" onClick={handleCheckCore} disabled={coreChecking || coreUpdating}>
                {coreChecking ? 'Проверка...' : 'Проверить обновление'}
              </button>
              {coreUpdate?.hasUpdate && (
                <button className="st-nat-btn" style={{ background: '#166534', color: '#ffffff' }} onClick={handleDoCoreUpdate} disabled={coreUpdating}>
                  <IconDownload size={14} /> {coreUpdating ? 'Обновление...' : 'Обновить ядро'}
                </button>
              )}
            </div>
          </div>

          <button
            className={`st-adv-toggle${advancedOpen ? ' st-adv-toggle--open' : ''}`}
            onClick={() => {
              if (!advancedOpen) setAdvancedConfirm(true);
              else setAdvancedOpen(false);
            }}
          >
            <span>Режим разработчика</span>
            <IconChevronDown stroke={2} size={16} />
          </button>

          <div className={`st-adv-body${advancedOpen ? ' st-adv-body--open' : ' st-adv-body--closed'}`}>
            <div className={`st-row${locked ? ' st-locked' : ''}`} style={{ marginTop: 10 }}>
              <span>MTU (Clamping)</span>
              <input
                type="number" min={576} max={1500} step={1}
                value={mtuRaw}
                className={`st-num-input${!mtuValid ? ' st-num-input--error' : ''}`}
                onChange={e => setMtuRaw(e.target.value)}
                onBlur={() => {
                  const n = Number(mtuRaw);
                  const clamped = Number.isFinite(n) ? Math.max(576, Math.min(1500, Math.round(n))) : 1300;
                  setMtuRaw(String(clamped));
                  update('mtu', clamped);
                }}
              />
            </div>

            <div className="st-nat-box">
              <div className="st-nat-title"><IconActivity size={14} /> Диагностика STUN NAT</div>
              {natResult ? (
                <>
                  <div className="st-nat-val">{natResult.natType}</div>
                  <div className="st-nat-sub">{natResult.details}</div>
                  {natResult.mappedIp && <div className="st-nat-sub">Внешний адрес: {natResult.mappedIp}:{natResult.mappedPort}</div>}
                </>
              ) : (
                <div className="st-nat-sub">Нажмите для проверки типа NAT в сети</div>
              )}
              <button className="st-nat-btn" onClick={handleCheckNAT} disabled={natLoading}>
                {natLoading ? 'Тестирование...' : 'Проверить тип NAT'}
              </button>
            </div>
          </div>
        </div>
      </div>

      {advancedConfirm && (
        <div className="st-confirm-overlay" onClick={() => setAdvancedConfirm(false)}>
          <div className="st-confirm" onClick={e => e.stopPropagation()}>
            <div className="st-confirm-title"><IconAlertTriangle size={15} /> Режим разработчика</div>
            <div className="st-confirm-text">
              Изменение системных параметров MTU/MSS Clamping и диагностика сети могут повлиять на работу туннеля.
              Продолжайте только если понимаете назначение этих опций.
            </div>
            <div className="st-confirm-actions">
              <button className="st-confirm-btn st-confirm-btn--cancel" onClick={() => setAdvancedConfirm(false)}>Отмена</button>
              <button className="st-confirm-btn st-confirm-btn--ok" onClick={() => { setAdvancedConfirm(false); setAdvancedOpen(true); }}>Продолжить</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
