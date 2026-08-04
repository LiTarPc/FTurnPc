export interface WdttLink {
  v?: number;
  provider?: string;
  peer: string;
  transport?: string;
  obf?: string;
  key?: string;
  cid?: string;
  name: string;
  wg?: string;
  links?: string;
}

export function parseWdttUrl(raw: string): WdttLink | null {
  try {
    let str = raw.trim();
    if (!str) return null;

    // 1. Direct raw JSON object check
    if (str.startsWith('{') && str.endsWith('}')) {
      try {
        const parsed = JSON.parse(str) as WdttLink;
        if (parsed && (parsed.peer || parsed.wg)) {
          if (!parsed.name) parsed.name = "Server";
          return parsed;
        }
      } catch {}
    }

    let linksUrl = "";
    const linksIdx = str.indexOf("-links");
    if (linksIdx !== -1) {
      let urlPart = str.slice(linksIdx + 6).trim();
      if ((urlPart.startsWith('"') && urlPart.endsWith('"')) || (urlPart.startsWith("'") && urlPart.endsWith("'"))) {
        urlPart = urlPart.slice(1, -1);
      }
      linksUrl = urlPart.trim();
      str = str.slice(0, linksIdx).trim();
    }

    if (str.startsWith('"') && str.endsWith('"')) {
      str = str.slice(1, -1);
    }
    if (str.startsWith('wdtt://')) {
      str = str.replace('wdtt://', '');
    } else if (str.startsWith('freeturn://')) {
      str = str.replace('freeturn://', '');
    }

    // 2. Base64 JSON decode
    try {
      const b64 = str.trim();
      const binString = atob(b64);
      const bytes = Uint8Array.from(binString, (m) => m.codePointAt(0) || 0);
      const jsonStr = new TextDecoder().decode(bytes);
      const parsed = JSON.parse(jsonStr) as WdttLink;
      if (linksUrl) {
        parsed.links = linksUrl;
      }
      if (!parsed.name) {
        parsed.name = "Server";
      }
      return parsed;
    } catch {}

    // 3. WireGuard .conf format check
    if (str.includes('[Interface]') || str.includes('[Peer]')) {
      return {
        name: "WG Server",
        peer: "127.0.0.1:9000",
        wg: str,
        links: linksUrl
      };
    }

    return null;
  } catch (e) {
    console.error("parseWdttUrl error", e);
    return null;
  }
}

type Listener = (link: WdttLink | null) => void;
let pending: WdttLink | null = null;
const listeners = new Set<Listener>();

export const wdttLinkStore = {
  subscribe: (fn: Listener) => { listeners.add(fn); fn(pending); return () => { listeners.delete(fn); }; },
  set: (link: WdttLink | null) => { pending = link; listeners.forEach(fn => fn(link)); },
  consume: () => { const l = pending; pending = null; listeners.forEach(fn => fn(null)); return l; },
};
