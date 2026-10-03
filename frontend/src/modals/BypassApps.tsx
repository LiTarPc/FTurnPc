import { useEffect, useMemo, useState } from 'react';
import { IconActivity, IconApps, IconDeviceFloppy, IconPlus, IconRefresh, IconTrash, IconX } from '@tabler/icons-react';
import { CheckNAT, GetBypassApps, GetRuCIDRStatus, SetBypassApps, UpdateRuCIDR } from '../../wailsjs/go/backend/App';
import { toastStore } from '../lib/stores/toastStore';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { settingsStore } from '../lib/store';
import { dnsServerError } from '../lib/dns';
import DnsServerControl from '../components/DnsServerControl';

interface Props {
  onClose: () => void;
}

type RuCIDRStatus = { count: number; source: string; updatedAt?: string };

function normalizeClientValue(raw: string): string {
  let value = raw.trim().replace(/^['"]|['"]$/g, '');
  if (!value) return '';
  value = value.replace(/\\/g, '/');
  const parts = value.split('/').filter(Boolean);
  return (parts[parts.length - 1] ?? '').trim();
}

export default function BypassApps({ onClose }: Props) {
  const [apps, setApps] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dnsRaw, setDnsRaw] = useState(settingsStore.get().dnsServer ?? '');
  const [bypassRu, setBypassRu] = useState(settingsStore.get().bypassRu);
  const [mtuRaw, setMtuRaw] = useState(String(settingsStore.get().mtu ?? 1300));
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  const [natResult, setNatResult] = useState<any>(null);
  const [natLoading, setNatLoading] = useState(false);
  const [ruStatus, setRuStatus] = useState<RuCIDRStatus | null>(null);
  const [ruUpdating, setRuUpdating] = useState(false);
  const mtu = Number(mtuRaw);
  const mtuValid = Number.isInteger(mtu) && mtu >= 576 && mtu <= 1500;
  const mtuLocked = tunnelState === 'connected' || tunnelState === 'connecting';

  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  useEffect(() => {
    GetBypassApps()
      .then((items: string[] | null | undefined) => setApps(Array.isArray(items) ? items : []))
      .catch((err: unknown) => {
        console.error(err);
        toastStore.show('Не удалось загрузить список bypass', 3500);
      })
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    GetRuCIDRStatus().then((status: RuCIDRStatus) => setRuStatus(status)).catch(console.error);
  }, []);

  const normalizedInput = useMemo(() => normalizeClientValue(input), [input]);

  const add = () => {
    const value = normalizedInput;
    if (!value) return;
    if (apps.some(item => item.toLowerCase() === value.toLowerCase())) {
      setInput('');
      return;
    }
    if (apps.length >= 128) {
      toastStore.show('Максимум 128 приложений', 2500);
      return;
    }
    setApps(prev => [...prev, value]);
    setInput('');
  };

  const remove = (name: string) => {
    setApps(prev => prev.filter(item => item !== name));
  };

  const checkNAT = async () => {
    setNatLoading(true);
    try {
      setNatResult(await CheckNAT());
    } catch (err: any) {
      setNatResult({ natType: 'Ошибка', details: err?.message || String(err) });
    } finally {
      setNatLoading(false);
    }
  };

  const updateRuCIDR = async () => {
    setRuUpdating(true);
    try {
      const status = await UpdateRuCIDR();
      setRuStatus(status);
      toastStore.show(`RU CIDR обновлены: ${status.count} сетей`, 3500);
    } catch (err: any) {
      toastStore.show(`Ошибка обновления RU CIDR: ${err?.message || String(err)}`, 5000);
    } finally {
      setRuUpdating(false);
    }
  };

  const save = async () => {
    const dnsError = dnsServerError(dnsRaw);
    if (dnsError) {
      toastStore.show(dnsError, 3500);
      return;
    }
    if (!mtuValid) {
      toastStore.show('MTU должен быть целым числом от 576 до 1500', 3500);
      return;
    }
    setSaving(true);
    try {
      await SetBypassApps(apps);
      settingsStore.save({ ...settingsStore.get(), bypassRu, dnsServer: dnsRaw.trim(), mtu });
      toastStore.show('Настройки обхода сохранены', 2500);
      onClose();
    } catch (err: any) {
      toastStore.show(`Ошибка сохранения: ${err?.message || String(err)}`, 4500);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <style>{`
        .bp-overlay {
          position: fixed;
          inset: 0;
          z-index: 120;
          display: flex;
          align-items: center;
          justify-content: center;
          background: var(--overlay-bg);
          animation: overlay-in 0.2s ease-out;
        }
        .bp-modal {
          width: 430px;
          max-width: 94vw;
          max-height: 86vh;
          display: flex;
          flex-direction: column;
          background: var(--popup-bg);
          border: 1px solid var(--border);
          border-radius: var(--border-radius);
          box-shadow: var(--shadow);
          padding: 18px;
          animation: modal-in 0.2s ease-out;
        }
        .bp-header {
          display: flex;
          align-items: center;
          gap: 9px;
          color: var(--text);
          margin-bottom: 8px;
        }
        .bp-body { min-height: 0; overflow-y: auto; padding-right: 3px; }
        .bp-section-title { color: var(--text); font-size: 12px; font-weight: 700; margin: 13px 0 8px; }
        .bp-setting-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--text); font-size: 12px; padding: 8px 0; }
        .bp-setting-hint { color: var(--text-3); font-size: 10px; line-height: 1.45; margin-top: 3px; }
        .bp-toggle { width: 44px; height: 24px; border: 0; border-radius: 50px; background: var(--toggle-off); position: relative; cursor: pointer; flex-shrink: 0; }
        .bp-toggle[aria-checked="true"] { background: var(--toggle-on); }
        .bp-toggle::after { content: ''; position: absolute; left: 4px; top: 4px; width: 16px; height: 16px; border-radius: 50%; background: var(--text-3); transition: left 0.2s; }
        .bp-toggle[aria-checked="true"]::after { left: 24px; background: #fff; }
        .bp-advanced { border-top: 1px solid var(--border); margin-top: 14px; padding-top: 12px; }
        .bp-advanced summary { color: var(--text-3); font-size: 12px; font-weight: 600; cursor: pointer; }
        .bp-mtu { width: 86px; padding: 6px 9px; border: 1px solid var(--input-border); border-radius: var(--border-radius); background: var(--input-bg); color: var(--text); font: inherit; font-size: 12px; text-align: right; }
        .bp-mtu:invalid { border-color: #ef4444; }
        .bp-nat { margin-top: 10px; padding: 10px; border-radius: var(--border-radius); background: var(--seg-bg); color: var(--text); font-size: 11px; }
        .bp-nat-result { color: var(--accent); font-weight: 700; margin: 5px 0 2px; }
        .bp-nat-button { width: 100%; margin-top: 8px; padding: 7px; border: 1px solid var(--border); border-radius: var(--border-radius); background: var(--button); color: var(--text); font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; }
        .bp-nat-button:disabled { opacity: 0.55; cursor: default; }
        .bp-title {
          flex: 1;
          font-size: 15px;
          font-weight: 700;
        }
        .bp-close {
          display: flex;
          align-items: center;
          justify-content: center;
          border: 0;
          background: transparent;
          color: var(--text-3);
          cursor: pointer;
          border-radius: 8px;
          padding: 5px;
        }
        .bp-close:hover { background: var(--button); color: var(--text); }
        .bp-description {
          color: var(--text-3);
          font-size: 11px;
          line-height: 1.45;
          margin-bottom: 14px;
        }
        .bp-input-row {
          display: flex;
          gap: 8px;
          margin-bottom: 12px;
        }
        .bp-input {
          flex: 1;
          min-width: 0;
          padding: 9px 11px;
          border: 1px solid var(--input-border);
          border-radius: var(--border-radius);
          background: var(--input-bg);
          color: var(--text);
          outline: none;
          font-family: var(--font);
          font-size: 12px;
        }
        .bp-input:focus { border-color: var(--input-focus); }
        .bp-add {
          width: 38px;
          border: 1px solid var(--border);
          border-radius: var(--border-radius);
          background: var(--button);
          color: var(--text);
          cursor: pointer;
          display: flex;
          align-items: center;
          justify-content: center;
        }
        .bp-add:hover { background: var(--button-hover); }
        .bp-list {
          min-height: 100px;
          max-height: 290px;
          overflow-y: auto;
          border: 1px solid var(--border);
          border-radius: var(--border-radius);
          background: var(--primary);
        }
        .bp-empty {
          min-height: 100px;
          display: flex;
          align-items: center;
          justify-content: center;
          color: var(--text-4);
          font-size: 12px;
        }
        .bp-item {
          display: flex;
          align-items: center;
          gap: 9px;
          padding: 9px 10px;
          border-bottom: 1px solid var(--border);
        }
        .bp-item:last-child { border-bottom: 0; }
        .bp-name {
          flex: 1;
          min-width: 0;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
          color: var(--text);
          font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
          font-size: 12px;
        }
        .bp-remove {
          border: 0;
          background: transparent;
          color: var(--text-3);
          cursor: pointer;
          padding: 5px;
          border-radius: 7px;
          display: flex;
          align-items: center;
          justify-content: center;
        }
        .bp-remove:hover { background: var(--button); color: #ef4444; }
        .bp-footer {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 10px;
          padding-top: 13px;
        }
        .bp-count { font-size: 10px; color: var(--text-4); }
        .bp-save {
          border: 0;
          border-radius: var(--border-radius);
          padding: 9px 14px;
          background: var(--accent);
          color: var(--accent-fg);
          font-family: var(--font);
          font-size: 12px;
          font-weight: 700;
          cursor: pointer;
          display: flex;
          align-items: center;
          gap: 7px;
        }
        .bp-save:disabled { opacity: 0.55; cursor: default; }
      `}</style>
      <div className="bp-overlay" onMouseDown={onClose}>
        <div className="bp-modal" onMouseDown={e => e.stopPropagation()}>
          <div className="bp-header">
            <IconApps size={20} stroke={2} />
            <span className="bp-title">Обход</span>
            <button className="bp-close" onClick={onClose} title="Закрыть">
              <IconX size={18} />
            </button>
          </div>

          <div className="bp-body">
            <div className="bp-setting-row">
              <div>
                <strong>Обход RU-ресурсов</strong>
                <div className="bp-setting-hint">Российские домены и адреса направляются напрямую.</div>
              </div>
              <button className="bp-toggle" role="switch" aria-label="Обход RU-ресурсов" aria-checked={bypassRu} onClick={() => setBypassRu(value => !value)} />
            </div>

            <div className="bp-nat">
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontWeight: 700 }}><IconRefresh size={14} /> RU CIDR</div>
              <div className="bp-setting-hint">
                {ruStatus ? (ruStatus.count > 0 ? `${ruStatus.source}: ${ruStatus.count} IPv4-сетей` : `${ruStatus.source}: список доступен в приложении`) : 'Загрузка сведений...'}
                {ruStatus?.updatedAt ? ` · обновлено ${new Date(ruStatus.updatedAt).toLocaleString('ru-RU')}` : ''}
              </div>
              <div className="bp-setting-hint">Источник: IPdeny. Обновление доступно без активного подключения; новые правила применятся при следующем подключении.</div>
              <button className="bp-nat-button" onClick={updateRuCIDR} disabled={ruUpdating || tunnelState !== 'idle'}>
                {ruUpdating ? 'Обновление...' : 'Обновить RU CIDR'}
              </button>
            </div>

            <DnsServerControl value={dnsRaw} onChange={setDnsRaw} />

            <div className="bp-section-title">Приложения напрямую</div>
            <div className="bp-description">
              Введите имя процесса, например <b>steam.exe</b>, или полный путь — сохранится только имя файла.
              Изменения применятся при следующем подключении.
            </div>

            <div className="bp-input-row">
              <input
                className="bp-input"
                value={input}
                disabled={loading}
                placeholder="steam.exe"
                onChange={e => setInput(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    add();
                  }
                }}
                autoFocus
              />
              <button className="bp-add" onClick={add} disabled={!normalizedInput || loading} title="Добавить">
                <IconPlus size={18} />
              </button>
            </div>

            <div className="bp-list">
              {loading ? (
                <div className="bp-empty">Загрузка...</div>
              ) : apps.length === 0 ? (
                <div className="bp-empty">Список пуст</div>
              ) : (
                apps.map(name => (
                  <div className="bp-item" key={name.toLowerCase()}>
                    <IconApps size={16} stroke={1.8} style={{ color: 'var(--text-3)' }} />
                    <span className="bp-name" title={name}>{name}</span>
                    <button className="bp-remove" onClick={() => remove(name)} title="Удалить">
                      <IconTrash size={16} />
                    </button>
                  </div>
                ))
              )}
            </div>

            <details className="bp-advanced">
              <summary>Дополнительно: MTU и диагностика NAT</summary>
              <div className="bp-setting-row">
                <div>
                  <strong>MTU (Clamping)</strong>
                  <div className="bp-setting-hint">Применится при следующем подключении.</div>
                </div>
                <input
                  className="bp-mtu"
                  type="number" min={576} max={1500} step={1}
                  value={mtuRaw}
                  disabled={mtuLocked}
                  onChange={event => setMtuRaw(event.target.value)}
                  onBlur={() => {
                    const value = Number(mtuRaw);
                    if (Number.isFinite(value)) setMtuRaw(String(Math.max(576, Math.min(1500, Math.round(value)))));
                  }}
                />
              </div>
              {mtuLocked && <div className="bp-setting-hint">MTU нельзя менять во время подключения.</div>}
              <div className="bp-nat">
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontWeight: 700 }}><IconActivity size={14} /> Диагностика STUN NAT</div>
                {natResult ? (
                  <>
                    <div className="bp-nat-result">{natResult.natType}</div>
                    <div className="bp-setting-hint">{natResult.details}</div>
                    {natResult.mappedIp && <div className="bp-setting-hint">Внешний адрес: {natResult.mappedIp}:{natResult.mappedPort}</div>}
                  </>
                ) : <div className="bp-setting-hint">Проверка типа NAT в сети.</div>}
                <button className="bp-nat-button" onClick={checkNAT} disabled={natLoading}>
                  {natLoading ? 'Тестирование...' : 'Проверить тип NAT'}
                </button>
              </div>
            </details>
          </div>

          <div className="bp-footer">
            <span className="bp-count">{apps.length} / 128</span>
            <button className="bp-save" onClick={save} disabled={loading || saving}>
              <IconDeviceFloppy size={16} />
              {saving ? 'Сохранение...' : 'Сохранить'}
            </button>
          </div>
        </div>
      </div>
    </>
  );
}
