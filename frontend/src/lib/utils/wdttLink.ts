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
    if (!str.startsWith('freeturn://')) return null;
    
    const b64 = str.replace('freeturn://', '').trim();
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
