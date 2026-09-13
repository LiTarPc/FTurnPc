import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  IconPower,
  IconTerminal2,
  IconSettings2,
  IconSun,
  IconMoon,
} from '@tabler/icons-react';
import { themeStore } from '../lib/stores/themeStore';

const NAV = [
  { path: '/', icon: <IconPower stroke={2} size={22} />, label: 'vpn' },
  { path: '/logs', icon: <IconTerminal2 stroke={2} size={22} />, label: 'логи' },
];

interface Props {
  onSettings?: () => void;
  pathname?: string;
}

export default function Sidebar({ onSettings, pathname: pathnameProp }: Props) {
  const navigate = useNavigate();
  const location = useLocation();
  const pathname = pathnameProp ?? location.pathname;
  const [theme, setTheme] = useState(() => themeStore.get());

  const toggleTheme = () => {
    themeStore.toggle();
    setTheme(themeStore.get());
  };

  return (
    <>
      <style>{`
        .sidebar {
          width: 80px;
          background: var(--sidebar-bg);
          display: flex;
          flex-direction: column;
          justify-content: space-between;
          padding: 12px 0;
          overflow: hidden;
          flex-shrink: 0;
          border-right: 1px solid var(--border);
        }
        .sidebar-top, .sidebar-bottom {
          display: flex;
          flex-direction: column;
          align-items: center;
          gap: 2px;
        }
        .nav-btn {
          width: 64px;
          padding: 10px 0 6px;
          border: none;
          border-radius: var(--border-radius);
          background: transparent;
          color: var(--text-3);
          cursor: pointer;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          gap: 3px;
          transition: background 0.15s, color 0.15s;
        }
        .nav-btn:hover {
          background: var(--button);
          color: var(--text);
        }
        .nav-btn--active {
          background: var(--accent);
          color: var(--accent-fg);
        }
        .nav-btn--active:hover {
          background: var(--accent-hover);
          color: var(--accent-fg);
        }
        .nav-label {
          font-size: 10px;
          font-weight: 500;
          letter-spacing: 0.3px;
          line-height: 1;
        }
        .theme-toggle {
          background: none;
          border: none;
          cursor: pointer;
          padding: 8px;
          display: flex;
          align-items: center;
          justify-content: center;
          color: var(--text-3);
          border-radius: var(--border-radius);
          transition: color 0.15s, background 0.15s;
        }
        .theme-toggle:hover {
          color: var(--text);
          background: var(--button);
        }
        .theme-toggle svg {
          transition: transform 0.3s ease;
        }
        .theme-toggle:active svg {
          transform: rotate(180deg) scale(0.85);
        }
      `}</style>
      <aside className="sidebar">
        <div className="sidebar-top">
          {NAV.map(({ path, icon, label }) => (
            <button
              key={path}
              className={`nav-btn${pathname === path ? ' nav-btn--active' : ''}`}
              onClick={() => navigate(path)}
            >
              {icon}
              <span className="nav-label">{label}</span>
            </button>
          ))}
        </div>
        <div className="sidebar-bottom">
          <button className="theme-toggle" onClick={toggleTheme} title="Сменить тему">
            {theme === 'light' ? <IconMoon size={18} stroke={2} /> : <IconSun size={18} stroke={2} />}
          </button>
          <button className="nav-btn" onClick={onSettings}>
            <IconSettings2 stroke={2} size={22} />
            <span className="nav-label">настр.</span>
          </button>
        </div>
      </aside>
    </>
  );
}
