import { useEffect, useMemo, useState } from 'react';
import { IconApps, IconDeviceFloppy, IconPlus, IconTrash, IconX } from '@tabler/icons-react';
import { GetBypassApps, SetBypassApps } from '../../wailsjs/go/backend/App';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  onClose: () => void;
}

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

  useEffect(() => {
    GetBypassApps()
      .then((items: string[] | null | undefined) => setApps(Array.isArray(items) ? items : []))
      .catch((err: unknown) => {
        console.error(err);
        toastStore.show('Не удалось загрузить список bypass', 3500);
      })
      .finally(() => setLoading(false));
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

  const save = async () => {
    setSaving(true);
    try {
      await SetBypassApps(apps);
      toastStore.show('Список bypass сохранён', 2500);
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
            <span className="bp-title">Bypass приложений</span>
            <button className="bp-close" onClick={onClose} title="Закрыть">
              <IconX size={18} />
            </button>
          </div>

          <div className="bp-description">
            Указанные процессы будут направляться напрямую, в обход VPN. Можно ввести имя процесса
            вроде <b>steam.exe</b> или вставить полный путь — будет сохранено только имя файла.
            Изменения применяются при следующем подключении или автоматическом переподключении.
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
