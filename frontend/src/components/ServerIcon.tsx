import type React from 'react';
import {
  IconCloverFilled, IconFlameFilled, IconShieldFilled, IconLayoutGridFilled, IconCloudFilled, IconBrandSpeedtest,
  IconStarFilled, IconHeartFilled, IconBoltFilled, IconRocket,
  IconCrownFilled, IconDiamondFilled, IconLeafFilled, IconSnowflake,
  IconServer, IconGlobe, IconLockFilled, IconWifi,
} from '@tabler/icons-react';

export const SERVER_ICONS: { key: string; render: (size: number) => React.ReactNode }[] = [
  { key: 'clover',     render: s => <IconCloverFilled size={s} /> },
  { key: 'flame',      render: s => <IconFlameFilled size={s} /> },
  { key: 'shield',     render: s => <IconShieldFilled size={s} /> },
  { key: 'grid',       render: s => <IconLayoutGridFilled size={s} /> },
  { key: 'cloud',      render: s => <IconCloudFilled size={s} /> },
  { key: 'speed',      render: s => <IconBrandSpeedtest size={s} stroke={2} /> },
  { key: 'star',       render: s => <IconStarFilled size={s} /> },
  { key: 'heart',      render: s => <IconHeartFilled size={s} /> },
  { key: 'bolt',       render: s => <IconBoltFilled size={s} /> },
  { key: 'rocket',     render: s => <IconRocket size={s} stroke={2} /> },
  { key: 'crown',      render: s => <IconCrownFilled size={s} /> },
  { key: 'diamond',    render: s => <IconDiamondFilled size={s} /> },
  { key: 'leaf',       render: s => <IconLeafFilled size={s} /> },
  { key: 'snowflake',  render: s => <IconSnowflake size={s} stroke={2} /> },
  { key: 'server',     render: s => <IconServer size={s} stroke={2} /> },
  { key: 'globe',      render: s => <IconGlobe size={s} stroke={2} /> },
  { key: 'lock',       render: s => <IconLockFilled size={s} /> },
  { key: 'wifi',       render: s => <IconWifi size={s} stroke={2} /> },
  { key: 'flag-ru',    render: () => <img src="/flags/ru.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-us',    render: () => <img src="/flags/us.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-de',    render: () => <img src="/flags/de.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-nl',    render: () => <img src="/flags/nl.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-fi',    render: () => <img src="/flags/fi.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-fr',    render: () => <img src="/flags/fr.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-gb',    render: () => <img src="/flags/gb.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-jp',    render: () => <img src="/flags/jp.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-pl',    render: () => <img src="/flags/pl.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-se',    render: () => <img src="/flags/se.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ch',    render: () => <img src="/flags/ch.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-lt',    render: () => <img src="/flags/lt.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-lv',    render: () => <img src="/flags/lv.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ee',    render: () => <img src="/flags/ee.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-cz',    render: () => <img src="/flags/cz.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-at',    render: () => <img src="/flags/at.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-ca',    render: () => <img src="/flags/ca.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-au',    render: () => <img src="/flags/au.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-sg',    render: () => <img src="/flags/sg.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-hk',    render: () => <img src="/flags/hk.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-tr',    render: () => <img src="/flags/tr.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
  { key: 'flag-kz',    render: () => <img src="/flags/kz.svg" style={{ width: '100%', height: '100%', objectFit: 'cover' }} /> },
];

export function ServerIcon({ iconKey, size }: { iconKey?: string; size: number }) {
  const entry = SERVER_ICONS.find(i => i.key === (iconKey ?? 'clover')) ?? SERVER_ICONS[0];
  return (
    <div style={{
      width: size,
      height: size,
      borderRadius: '5px',
      overflow: 'hidden',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      flexShrink: 0,
    }}>
      {entry.render(size)}
    </div>
  );
}
