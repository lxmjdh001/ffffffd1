import { useState } from 'react';
import { Settings2 } from 'lucide-react';
import type { App } from './types';
import { AppDownloadSettings } from './app-download-settings';
import { DomainShareDrawer } from './domain-share-drawer';

export function AppShareDrawer({ app, close }: { app: App; close: () => void }) {
 const [settingsOpen,setSettingsOpen]=useState(false);
 const [downloadMode,setDownloadMode]=useState(app.download_mode || 'direct');
 return <>
  <DomainShareDrawer path={`/a/${app.slug}`} close={close} settingsAction={<button type="button" className="btn secondary" onClick={()=>setSettingsOpen(true)}><Settings2 size={15}/>设置</button>}/>
  {settingsOpen&&<AppDownloadSettings app={{...app,download_mode:downloadMode}} close={()=>setSettingsOpen(false)} onSaved={updated=>setDownloadMode(updated.download_mode || 'direct')}/>}
 </>;
}
