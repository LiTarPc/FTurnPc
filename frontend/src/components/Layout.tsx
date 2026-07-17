import { useState, useEffect } from 'react';
import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import Sidebar from './Sidebar';
import Settings from '../modals/Settings';

export default function Layout() {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const loc = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    if (loc.pathname !== '/' && loc.pathname !== '/logs' && loc.pathname !== '/settings') {
      navigate('/');
    }
  }, [loc.pathname, navigate]);

  return (
    <>
      <style>{`
        .layout { display: flex; height: 100vh; background: var(--bg); color: var(--text); overflow: hidden; }
        .content { flex: 1; position: relative; overflow-y: auto; overflow-x: hidden; display: flex; flex-direction: column; }
      `}</style>
      <div className="layout">
        <Sidebar
          onSettings={() => setSettingsOpen(true)}
        />
        <div className="content">
          <Outlet />
        </div>
      </div>
      {settingsOpen && <Settings onClose={() => setSettingsOpen(false)} />}
    </>
  );
}
