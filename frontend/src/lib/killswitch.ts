import { GetDetectedBrowsers, SetKillSwitchConfig } from '../../wailsjs/go/backend/App';
import type { AppSettings } from './types';

export interface BrowserItem {
  id: string;
  name: string;
  exePath: string;
  enabled?: boolean;
}

export function getActiveProtectedBrowsers(
  detected: BrowserItem[],
  disabled: string[],
  custom: string[]
): string[] {
  const disabledSet = new Set(disabled || []);
  const result: string[] = [];
  for (const b of detected) {
    if (!disabledSet.has(b.id) && !disabledSet.has(b.exePath)) {
      if (b.exePath && !result.includes(b.exePath)) {
        result.push(b.exePath);
      }
    }
  }
  for (const p of custom || []) {
    if (!disabledSet.has(p) && !result.includes(p)) {
      result.push(p);
    }
  }
  return result;
}

export async function syncKillSwitchConfig(settings: AppSettings, detectedBrowsers?: BrowserItem[]) {
  try {
    const list = (detectedBrowsers && detectedBrowsers.length > 0) ? detectedBrowsers : (await GetDetectedBrowsers());
    const active = getActiveProtectedBrowsers(list, settings.disabledBrowsers || [], settings.customBrowsers || []);
    await SetKillSwitchConfig(!!settings.browserKillSwitch, settings.killSwitchMode || 'reconnect', active);
  } catch (e) {
    console.error('Failed to sync kill switch config:', e);
  }
}
