import { useState, useEffect, useRef, useCallback } from 'react';
import { IconSearch, IconTrashX, IconCopy } from '@tabler/icons-react';
import { logStore, type LogEntry, type LogLevel } from '../lib/stores/logStore';

type Filter = 'ALL' | 'INFO' | 'ERROR';

const LEVEL_COLOR: Record<LogLevel, string> = {
  INFO: 'var(--text)',
  WARN: '#f59e0b',
  ERROR: '#ef4444',
  DEBUG: 'var(--text-3)',
};

function cleanLogMessage(msg: string): string {
  // Matches "2026/07/18 17:25:51 [DEBUG] " or "2026/07/18 17:25:51 [INFO] " or similar
  const prefixRegex = /^\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2}:\d{2}\s+\[(?:DEBUG|INFO|WARN|ERROR)\]\s*/i;
  return msg.replace(prefixRegex, '');
}

export default function Logs() {
  const [filter, setFilter] = useState<Filter>('ALL');
  const [search, setSearch] = useState('');
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const autoScroll = useRef(true);

  useEffect(() => logStore.subscribe(setEntries), []);

  useEffect(() => {
    if (autoScroll.current) bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [entries]);

  const onScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }, []);

  const visible = entries.filter(e => {
    if (filter !== 'ALL' && e.level !== filter) return false;
    if (search && !e.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const handleCopy = () => {
    const text = visible.map(e => `[${e.time}] [${e.level}] ${cleanLogMessage(e.message)}${e.count > 1 ? ` (×${e.count})` : ''}`).join('\n');
    navigator.clipboard.writeText(text);
  };

  return (
    <>
      <style>{`
        .logs-main {
          flex: 1;
          min-height: 0;
          padding: 16px 20px 24px 20px;
          display: flex;
          flex-direction: column;
          animation: page-in 0.25s ease-out;
          background: var(--primary);
        }
        .logs-card {
          flex: 1;
          min-height: 0;
          border: 1px solid var(--border);
          border-radius: var(--border-radius);
          display: flex;
          flex-direction: column;
          overflow: hidden;
          background: var(--primary);
        }
        .logs-toolbar {
          display: flex;
          flex-wrap: wrap;
          align-items: center;
          gap: 10px;
          padding: 12px 14px;
          border-bottom: 1px solid var(--border);
          flex-shrink: 0;
        }
        .search-wrap {
          flex: 1;
          display: flex;
          justify-content: center;
          min-width: 140px;
        }
        .search-inner {
          position: relative;
          width: 100%;
          max-width: 380px;
        }
        .search-input {
          width: 100%;
          padding: 8px 36px 8px 14px;
          border: 1.5px solid var(--input-border);
          border-radius: var(--border-radius);
          background: var(--input-bg);
          font-size: 13px;
          color: var(--text);
          outline: none;
          box-sizing: border-box;
          transition: border-color 0.15s;
        }
        .search-input:focus {
          border-color: var(--input-focus);
        }
        .search-input::placeholder {
          color: var(--text-4);
        }
        .search-icon {
          position: absolute;
          right: 12px;
          top: 50%;
          transform: translateY(-50%);
          color: var(--text-3);
          pointer-events: none;
        }
        .logs-toolbar-right {
          display: flex;
          align-items: center;
          gap: 10px;
          flex-shrink: 0;
          flex-wrap: wrap;
        }
        .filter-group {
          display: flex;
          background: var(--seg-bg);
          border-radius: var(--border-radius);
          padding: 3px;
          gap: 2px;
        }
        .filter-btn {
          padding: 6px 16px;
          border: none;
          border-radius: calc(var(--border-radius) - 3px);
          background: transparent;
          font-size: 12px;
          font-weight: 600;
          color: var(--seg-text);
          cursor: pointer;
          transition: background 0.15s, color 0.15s;
        }
        .filter-btn--active {
          background: var(--accent);
          color: var(--accent-fg);
        }
        .icon-btn {
          width: 36px;
          height: 36px;
          border: 1px solid var(--border);
          border-radius: var(--border-radius);
          background: var(--button);
          cursor: pointer;
          display: flex;
          align-items: center;
          justify-content: center;
          color: var(--text-2);
          transition: background 0.12s, border-color 0.12s, color 0.12s;
        }
        .icon-btn:hover {
          background: var(--button-hover);
          border-color: var(--border);
          color: var(--text);
        }
        .icon-btn:active {
          background: var(--button-press);
        }
        .logs-list {
          flex: 1;
          min-height: 0;
          overflow-y: auto;
          padding: 8px 0;
        }
        .log-row {
          display: flex;
          flex-direction: column;
          padding: 8px 16px;
          border-bottom: 1px solid var(--border-2);
          gap: 4px;
        }
        .log-row:last-child {
          border-bottom: none;
        }
        .log-row:hover {
          background: var(--button);
        }
        .log-header-line {
          display: flex;
          align-items: flex-start;
          gap: 8px;
          width: 100%;
        }
        .log-level {
          flex-shrink: 0;
          font-weight: 700;
          font-size: 11px;
          width: 44px;
        }
        .log-msg {
          flex: 1;
          font-size: 12px;
          line-height: 1.4;
          word-break: break-word;
          color: var(--text);
        }
        .log-meta-line {
          display: flex;
          justify-content: space-between;
          align-items: center;
          width: 100%;
          padding-left: 52px;
        }
        .log-time {
          color: var(--text-4);
          font-size: 11px;
          font-variant-numeric: tabular-nums;
        }
        .log-count {
          flex-shrink: 0;
          background: var(--seg-bg);
          border-radius: 20px;
          padding: 1px 7px;
          font-size: 10px;
          color: var(--text-2);
        }
        .logs-empty {
          flex: 1;
          display: flex;
          align-items: center;
          justify-content: center;
          color: var(--text-4);
          font-size: 14px;
        }
      `}</style>
      <main className="logs-main">
        <div className="logs-card">
          <div className="logs-toolbar">
            <div className="search-wrap">
              <div className="search-inner">
                <input
                  className="search-input"
                  placeholder="Поиск...."
                  value={search}
                  onChange={e => setSearch(e.target.value)}
                />
                <IconSearch size={18} className="search-icon" />
              </div>
            </div>
            <div className="logs-toolbar-right">
              <div className="filter-group">
                {(['ALL', 'INFO', 'ERROR'] as Filter[]).map(f => (
                  <button key={f} className={`filter-btn${filter === f ? ' filter-btn--active' : ''}`} onClick={() => setFilter(f)}>{f}</button>
                ))}
              </div>
              <button className="icon-btn" onClick={logStore.clear} title="Очистить" aria-label="Очистить логи">
                <IconTrashX stroke={2} size={16} />
              </button>
              <button className="icon-btn" onClick={handleCopy} title="Копировать" aria-label="Копировать логи">
                <IconCopy stroke={2} size={16} />
              </button>
            </div>
          </div>

          {visible.length === 0 ? (
            <div className="logs-empty">{entries.length === 0 ? 'Логи появятся здесь...' : 'Ничего не найдено'}</div>
          ) : (
            <div className="logs-list" ref={listRef} onScroll={onScroll}>
              {visible.map(e => (
                <div key={e.id} className="log-row">
                  <div className="log-header-line">
                    <span className="log-level" style={{ color: LEVEL_COLOR[e.level] }}>{e.level}</span>
                    <span className="log-msg">{cleanLogMessage(e.message)}</span>
                  </div>
                  <div className="log-meta-line">
                    <span className="log-time">{e.time}</span>
                    {e.count > 1 && <span className="log-count">×{e.count}</span>}
                  </div>
                </div>
              ))}
              <div ref={bottomRef} />
            </div>
          )}
        </div>
      </main>
    </>
  );
}