export function dnsServerError(raw: string): string | null {
  const value = raw.trim();
  if (!value) return null;

  const ipv4 = value.split('.');
  if (ipv4.length === 4 && ipv4.every(part => /^\d{1,3}$/.test(part) && (part.length === 1 || !part.startsWith('0')) && Number(part) <= 255)) return null;
  if (value.includes(':') && !value.includes('://')) {
    try { new URL(`http://[${value}]`); return null; } catch { /* continue */ }
  }

  let url: URL;
  try { url = new URL(value); } catch { return 'Введите IPv4 или адрес udp://, tls://, https://'; }
  if (!['udp:', 'tls:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash) {
    return 'Введите IPv4 или адрес udp://, tls://, https://';
  }
  const host = url.hostname.replace(/^\[|\]$/g, '');
  if ((!host.includes(':') && !/^[a-z0-9.-]+$/i.test(host)) || host.startsWith('.') || host.endsWith('.') || host.includes('..')) {
    return 'Неверный адрес DNS сервера';
  }
  if (url.port && (Number(url.port) < 1 || Number(url.port) > 65535)) return 'Неверный порт DNS сервера';
  if (url.protocol !== 'https:' && url.pathname !== '/' && url.pathname !== '') return 'Путь поддерживается только для DoH';
  return null;
}
