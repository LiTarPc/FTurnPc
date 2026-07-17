import { useState } from 'react';
import { IconHash, IconX } from '@tabler/icons-react';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  hashes: [string, string, string, string];
  onClose: () => void;
  onSave: (hashes: [string, string, string, string]) => void;
}

export default function Hash({ hashes, onClose, onSave }: Props) {
  const [values, setValues] = useState<[string, string, string, string]>([...hashes] as [string, string, string, string]);

  const normalize = (v: string) => {
    const idx = v.toLowerCase().indexOf('/call/join/');
    if (idx !== -1) v = v.slice(idx + '/call/join/'.length);
    return v.split(/[?#/]/)[0].trim();
  };

  const set = (i: number, v: string) => {
    const next = [...values] as [string, string, string, string];
    next[i] = v;
    setValues(next);
  };

  const save = () => {
    const normalized = values.map(normalize) as [string, string, string, string];
    const nonEmpty = normalized.filter(v => v !== '');
    if (new Set(nonEmpty).size !== nonEmpty.length) {
      toastStore.show('Обнаружены дублирующиеся хеши');
      return;
    }
    onSave(normalized);
    onClose();
  };

  return (
    <>
      <style>{`
        .hash-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .hash-modal { background: var(--surface); border-radius: 14px; padding: 20px; width: 380px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .hash-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .hash-title { font-size: 16px; font-weight: 600; flex: 1; color: var(--text); }
        .hash-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .hash-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: 10px; font-size: 14px; font-family: 'Geist', sans-serif; outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .hash-input::placeholder { color: var(--text-4); }
        .hash-btn { width: 100%; padding: 13px; border: none; border-radius: 10px; background: var(--accent); color: var(--accent-fg); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; margin-top: 4px; }
      `}</style>
      <div className="hash-overlay">
        <div className="hash-modal" onClick={e => e.stopPropagation()}>
          <div className="hash-header">
            <IconHash stroke={2} size={22} />
            <span className="hash-title">Hash</span>
            <button className="hash-close" onClick={onClose}><IconX size={18} /></button>
          </div>
          {values.map((v, i) => (
            <input key={i} className="hash-input" placeholder={`Hash - ${i + 1}`} value={v} onChange={e => set(i, e.target.value)} />
          ))}
          <button className="hash-btn" onClick={save}>Сохранить</button>
        </div>
      </div>
    </>
  );
}
