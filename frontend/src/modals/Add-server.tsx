import { useState } from 'react';
import { IconCircleHalf2, IconX } from '@tabler/icons-react';
import type { Server } from '../lib/types';
import { SaveProfile } from '../../wailsjs/go/backend/App';
import { parseWdttUrl } from '../lib/utils/wdttLink';

interface Props {
  onClose: () => void;
  onAdd: (server: Omit<Server, 'id'>) => void;
}

export default function AddServer({ onClose, onAdd }: Props) {
  const [link, setLink] = useState('');
  const [parsed, setParsed] = useState<any>(null);
  const [name, setName] = useState('');

  const applyLink = (raw: string) => {
    setLink(raw);
    const p = parseWdttUrl(raw.trim());
    if (p) {
      setParsed(p);
      setName(p.name || 'Server');
    } else {
      setParsed(null);
    }
  };

  const handleAdd = async () => {
    if (!parsed || !name.trim()) return;

    try {
      await SaveProfile(name.trim(), {
        name: name.trim(),
        provider: parsed.provider || '',
        peer: parsed.peer || '',
        transport: parsed.transport || '',
        obf: parsed.obf || '',
        key: parsed.key || '',
        cid: parsed.cid || '',
        wg: parsed.wg || '',
        links: parsed.links || '',
      } as any);
    } catch (e) {
      console.warn('SaveProfile failed:', e);
    }

    onAdd({
      name: name.trim(),
      host: parsed.peer || '',
      password: parsed.key || '',
      provider: parsed.provider || '',
      peer: parsed.peer || '',
      transport: parsed.transport || '',
      obf: parsed.obf || '',
      key: parsed.key || '',
      cid: parsed.cid || '',
      wg: parsed.wg || '',
      links: parsed.links || '',
    });
    onClose();
  };

  return (
    <>
      <style>{`
        .as-overlay { position: fixed; inset: 0; background: var(--overlay-bg); display: flex; align-items: center; justify-content: center; z-index: 100; animation: overlay-in 0.3s ease-out; }
        .as-modal { background: var(--popup-bg); border-radius: var(--border-radius); padding: 20px; width: 340px; max-width: 90vw; box-shadow: var(--shadow); border: 1px solid var(--border); max-height: 85vh; overflow-y: auto; animation: modal-in 0.3s ease-out; }
        .as-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .as-title { font-size: 15px; font-weight: 600; flex: 1; color: var(--text); }
        .as-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .as-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: var(--border-radius); font-size: 13px; font-family: var(--font); outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); transition: border-color 0.15s; }
        .as-input:focus { border-color: var(--input-focus); }
        .as-input::placeholder { color: var(--text-4); }
        .as-btn { width: 100%; padding: 13px; border: none; border-radius: var(--border-radius); background: var(--accent); color: var(--accent-fg); font-size: 13px; font-family: var(--font); font-weight: 600; cursor: pointer; margin-top: 4px; }
        .as-btn:disabled { opacity: 0.4; cursor: not-allowed; }
      `}</style>
      <div className="as-overlay">
        <div className="as-modal" onClick={e => e.stopPropagation()}>
          <div className="as-header">
            <IconCircleHalf2 stroke={2} size={22} />
            <span className="as-title">Добавление сервера</span>
            <button className="as-close" onClick={onClose}><IconX size={18} /></button>
          </div>

          <input
            className="as-input"
            placeholder="Вставьте ссылку freeturn://..."
            value={link}
            onChange={e => applyLink(e.target.value)}
          />

          {parsed && (
            <input 
              className="as-input" 
              placeholder="Название сервера" 
              value={name} 
              onChange={e => setName(e.target.value)} 
            />
          )}
          
          <button className="as-btn" onClick={handleAdd} disabled={!parsed || !name.trim()}>Добавить сервер</button>
        </div>
      </div>
    </>
  );
}
