import { useState, useEffect } from 'react';
import { SaveProfile, GetProfile } from '../../wailsjs/go/backend/App';
import type { Server } from '../lib/types';
import { serverStore } from '../lib/store';
import { toastStore } from '../lib/stores/toastStore';

interface Props {
  server: Server;
  onClose: () => void;
  onSave?: (updated: Server) => void;
}

export function ViewServer({ server, onClose, onSave }: Props) {
  const [links, setLinks] = useState(server.links || '');
  const [power, setPower] = useState(String(server.power || 10));
  const [streams, setStreams] = useState(String(server.streamsPerCred || 5));
  
  const [peer, setPeer] = useState(server.peer || '');
  const [provider, setProvider] = useState(server.provider || '');
  const [transport, setTransport] = useState(server.transport || 'tcp');
  const [obf, setObf] = useState(server.obf || '');
  const [obfKey, setObfKey] = useState(server.key || '');
  const [cid, setCid] = useState(server.cid || '');
  const [wg, setWg] = useState(server.wg || '');

  const [devMode, setDevMode] = useState(false);
  const [profile, setProfile] = useState<Server>(server);

  useEffect(() => {
    GetProfile(server.name).then((data: any) => {
      if (!data) return;
      const full: Server = { ...server, ...data };
      setProfile(full);
      setLinks(data.links || server.links || '');
      setPower(String(data.power || server.power || 10));
      setStreams(String(data.streamsPerCred || server.streamsPerCred || 5));
      setPeer(data.peer || server.peer || '');
      setProvider(data.provider || server.provider || '');
      setTransport(data.transport || server.transport || 'tcp');
      setObf(data.obf || server.obf || '');
      setObfKey(data.key || server.key || '');
      setCid(data.cid || server.cid || '');
      setWg(data.wg || server.wg || '');
    }).catch(console.error);
  }, [server.name]);

  const handleSave = async () => {
    try {
      const pNum = parseInt(power, 10);
      const sNum = parseInt(streams, 10);
      
      const next: Server = {
        ...profile,
        links: links.trim(),
        power: isNaN(pNum) ? 10 : pNum,
        streamsPerCred: isNaN(sNum) ? 5 : sNum,
        peer: peer.trim(),
        provider: provider.trim(),
        transport: transport.trim(),
        obf: obf.trim(),
        key: obfKey.trim(),
        cid: cid.trim(),
        wg: wg.trim(),
      };

      await SaveProfile(next.name, next as any);
      
      // Update store
      serverStore.update(next);
      
      if (onSave) {
        onSave(next);
      }
      
      toastStore.show('Профиль сохранен');
      onClose();
    } catch (e: any) {
      toastStore.show('Ошибка сохранения: ' + e);
    }
  };

  return (
    <>
      <style>{`
        .modal-overlay {
          position: fixed;
          inset: 0;
          background: var(--overlay-bg);
          display: flex;
          align-items: center;
          justify-content: center;
          z-index: 100;
          animation: overlay-in 0.3s ease-out;
        }
        .modal {
          background: var(--popup-bg);
          border-radius: var(--border-radius);
          padding: 20px;
          width: 340px;
          max-width: 90vw;
          box-shadow: var(--shadow);
          border: 1px solid var(--border);
          max-height: 85vh;
          display: flex;
          flex-direction: column;
          animation: modal-in 0.3s ease-out;
          color: var(--text);
          box-sizing: border-box;
        }
        .modal-header {
          display: flex;
          align-items: center;
          justify-content: space-between;
          margin-bottom: 15px;
          color: var(--text);
        }
        .modal-header h2 {
          margin: 0;
          font-weight: 600;
        }
        .modal-body {
          flex: 1;
          overflow-y: auto;
          display: flex;
          flex-direction: column;
          gap: 1rem;
          max-height: 65vh;
          padding-right: 4px;
        }
        .modal-footer {
          display: flex;
          justify-content: flex-end;
          gap: 10px;
          margin-top: 15px;
          padding-top: 10px;
          border-top: 1px solid var(--border);
        }
        .form-group {
          display: flex;
          flex-direction: column;
          gap: 6px;
        }
        .form-group.row {
          flex-direction: row;
          align-items: center;
          justify-content: space-between;
        }
        .form-group label {
          font-size: 13px;
          color: var(--text-2);
        }
        .form-group.row label {
          flex: 1;
        }
        .form-group.row .input {
          flex: 2;
        }
        .input {
          padding: 8px 12px;
          border: 1.5px solid var(--input-border);
          border-radius: var(--border-radius);
          font-size: 13px;
          background: var(--input-bg);
          color: var(--text);
          outline: none;
          width: 100%;
          box-sizing: border-box;
          transition: border-color 0.15s;
        }
        .input:focus {
          border-color: var(--input-focus);
        }
        .btn {
          padding: 8px 16px;
          border-radius: var(--border-radius);
          font-size: 13px;
          font-weight: 600;
          cursor: pointer;
          border: none;
          transition: background-color 0.2s;
        }
        .btn:hover {
          opacity: 0.9;
        }
        .btn-close {
          background: none;
          border: none;
          cursor: pointer;
          font-size: 18px;
          color: var(--text-3);
          display: flex;
          align-items: center;
          justify-content: center;
          padding: 4px;
        }
        .btn-close:hover {
          color: var(--text);
        }
        hr {
          border: 0;
          border-top: 1px solid var(--border);
          margin: 0.5rem 0;
        }
      `}</style>
      <div className="modal-overlay" onMouseDown={onClose}>
        <div className="modal" onMouseDown={e => e.stopPropagation()}>
          <div className="modal-header">
            <h2 style={{fontSize: 20}}>Профиль: {profile.name}</h2>
            <button className="btn-close" onClick={onClose}>✕</button>
          </div>
          <div className="modal-body">
            
            <div className="form-group row">
              <label>VK Call (Links):</label>
              <input 
                type="text" 
                className="input" 
                value={links} 
                onChange={e => setLinks(e.target.value)}
                placeholder="vk.ru/call..."
              />
            </div>

            <div className="form-group row">
              <label>Threads (-n):</label>
              <input 
                type="number" 
                className="input" 
                value={power} 
                onChange={e => setPower(e.target.value)}
                min="1"
                max="100"
              />
            </div>

            <div className="form-group row">
              <label>Streams/cred:</label>
              <input 
                type="number" 
                className="input" 
                value={streams} 
                onChange={e => setStreams(e.target.value)}
                min="1"
                max="20"
              />
            </div>

            <div className="form-group row" style={{ marginTop: 14, marginBottom: 4 }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '13px', fontWeight: 600, color: 'var(--text-3)' }}>
                <input 
                  type="checkbox" 
                  checked={devMode} 
                  onChange={e => setDevMode(e.target.checked)} 
                  style={{ width: 16, height: 16, cursor: 'pointer' }}
                />
                <span>Режим разработчика</span>
              </label>
            </div>

            {devMode && (
              <>
                <hr />

                <div className="form-group row">
                  <label>Peer (Address):</label>
                  <input type="text" className="input" value={peer} onChange={e => setPeer(e.target.value)} />
                </div>

                <div className="form-group row">
                  <label>Provider:</label>
                  <input type="text" className="input" value={provider} onChange={e => setProvider(e.target.value)} />
                </div>

                <div className="form-group row">
                  <label>Transport:</label>
                  <input type="text" className="input" value={transport} onChange={e => setTransport(e.target.value)} />
                </div>

                <div className="form-group row">
                  <label>Obf Profile:</label>
                  <input type="text" className="input" value={obf} onChange={e => setObf(e.target.value)} />
                </div>

                <div className="form-group row">
                  <label>Obf Key:</label>
                  <input type="text" className="input" value={obfKey} onChange={e => setObfKey(e.target.value)} />
                </div>

                <div className="form-group row">
                  <label>Client ID:</label>
                  <input type="text" className="input" value={cid} onChange={e => setCid(e.target.value)} />
                </div>

                <hr />

                <div className="form-group" style={{flexDirection: 'column', alignItems: 'flex-start'}}>
                  <label style={{marginBottom: '0.5rem'}}>WG Config:</label>
                  <textarea 
                    className="input" 
                    value={wg} 
                    onChange={e => setWg(e.target.value)}
                    style={{height: 120, width: '100%', fontFamily: 'monospace', resize: 'none', whiteSpace: 'pre', fontSize: '11px'}} 
                  />
                </div>
              </>
            )}

          </div>
          <div className="modal-footer">
            <button className="btn" onClick={onClose} style={{background: 'transparent', border: '1px solid var(--border)', color: 'var(--text)'}}>Закрыть</button>
            <button className="btn" onClick={handleSave} style={{background: 'var(--accent)', color: 'var(--accent-fg)'}}>Сохранить</button>
          </div>
        </div>
      </div>
    </>
  );
}
