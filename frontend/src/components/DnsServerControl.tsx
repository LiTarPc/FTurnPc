import { dnsServerError } from '../lib/dns';

interface Props {
  value: string;
  onChange: (value: string) => void;
  onBlur?: () => void;
}

export default function DnsServerControl({ value, onChange, onBlur }: Props) {
  const error = dnsServerError(value);
  return (
    <div style={{ padding: '10px 0', borderBottom: '1px solid var(--border)' }}>
      <style>{`
        .dns-preset { padding: 6px 9px; border: 1px solid var(--border); border-radius: var(--border-radius); background: var(--button); color: var(--text); cursor: pointer; font: inherit; font-size: 11px; }
        .dns-preset--active { border-color: var(--accent); font-weight: 700; }
      `}</style>
      <label htmlFor="singbox-dns" style={{ display: 'block', fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>
        DNS для sing-box
      </label>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', margin: '8px 0' }}>
        <button type="button" className={`dns-preset${value.trim() ? '' : ' dns-preset--active'}`} onClick={() => onChange('')}>По умолчанию</button>
        <button type="button" className={`dns-preset${value.trim() === '94.140.14.14' ? ' dns-preset--active' : ''}`} onClick={() => onChange('94.140.14.14')}>AdGuard DNS</button>
        <button type="button" className={`dns-preset${value.trim() === 'https://dns.adguard-dns.com/dns-query' ? ' dns-preset--active' : ''}`} onClick={() => onChange('https://dns.adguard-dns.com/dns-query')}>AdGuard DoH</button>
      </div>
      <input
        id="singbox-dns"
        type="text"
        value={value}
        placeholder="94.140.14.14 или https://dns.adguard-dns.com/dns-query"
        onChange={event => onChange(event.target.value)}
        onBlur={onBlur}
        spellCheck={false}
        autoComplete="off"
        style={{ width: '100%', boxSizing: 'border-box', padding: '8px 10px', borderRadius: 'var(--border-radius)', border: `1px solid ${error ? '#ef4444' : 'var(--border)'}`, background: 'var(--input-bg)', color: 'var(--text)', fontSize: 12 }}
      />
      {error && <div style={{ color: '#ef4444', fontSize: 11, marginTop: 4 }}>{error}</div>}
      <div style={{ color: 'var(--text-3)', fontSize: 11, marginTop: 5 }}>
        По умолчанию: DNS WG-профиля или 1.1.1.1. Применится при следующем подключении; DNS FreeTurn не меняется.
      </div>
    </div>
  );
}
