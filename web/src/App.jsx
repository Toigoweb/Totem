import { useEffect } from 'preact/hooks';
import { app } from './app.js';
import { useStore } from './store.js';
import { Icon } from './components/Icon.jsx';
import { Home } from './screens/Home.jsx';
import { Search } from './screens/Search.jsx';
import { Section } from './screens/Section.jsx';
import { Reader } from './screens/Reader.jsx';
import { Status } from './screens/Status.jsx';
import { Pin } from './screens/Pin.jsx';
import { Maint, MaintForm, ChangePin } from './screens/Maint.jsx';
import { Wizard } from './screens/Wizard.jsx';
import { Devices, PairStation, Problems } from './screens/Devices.jsx';
import { Screensaver } from './kiosk/Screensaver.jsx';
import { Emergency } from './kiosk/Emergency.jsx';
import { EmergencyAdmin } from './screens/EmergencyAdmin.jsx';
import { DeviceControl } from './screens/DeviceControl.jsx';
import { Uploads } from './screens/Uploads.jsx';
import { SafetyMap } from './screens/SafetyMap.jsx';
import { SafetyAdmin } from './screens/SafetyAdmin.jsx';
import { Intercom, PhoneHome } from './screens/Intercom.jsx';
import { IntercomAdmin } from './screens/IntercomAdmin.jsx';
import { CallOverlay } from './intercom/CallOverlay.jsx';
import { PttBar } from './intercom/PttBar.jsx';
import { startIntercom, stopIntercom } from './intercom/client.js';
import { Departments, Contacts, Notices, ScreensaverSettings, Monitoring } from './screens/Extras.jsx';

const SCREENS = { home: Home, search: Search, section: Section, reader: Reader, status: Status, pin: Pin, maint: Maint, form: MaintForm, changePin: ChangePin, wizard: Wizard, devices: Devices, pairStation: PairStation, problems: Problems, uploads: Uploads, safety: SafetyMap, safetyAdmin: SafetyAdmin, emergencyAdmin: EmergencyAdmin, kioskDevice: DeviceControl, intercom: Intercom, intercomAdmin: IntercomAdmin,
  departments: Departments, contacts: Contacts, notices: Notices, screensaverSettings: ScreensaverSettings, monitoring: Monitoring };
const MAINT_ONLY = new Set(['maint', 'form', 'changePin', 'devices', 'pairStation', 'problems', 'uploads', 'safetyAdmin', 'emergencyAdmin', 'kioskDevice', 'intercomAdmin', 'departments', 'contacts', 'notices', 'screensaverSettings', 'monitoring']);

export function App() {
  const s = useStore(app);
  useEffect(() => {
    document.documentElement.dataset.scale = s.scale;
  }, [s.scale]);
  // interfono di totem e postazioni: solo se il manifest lo prevede per questo dispositivo
  // (il telefono lo apre dalla sua Home)
  const icOn = !!(s.paired && s.role !== 'telefono' && s.extras.intercom && s.extras.intercom.enabled);
  useEffect(() => {
    if (icOn) startIntercom();
    else if (s.role !== 'telefono') stopIntercom();
  }, [icOn]);
  if (!s.ready) return null;
  let name = s.route.name;
  if (MAINT_ONLY.has(name) && !s.maint) name = 'home'; // manutenzione scaduta
  if (name === 'intercom' && !icOn) name = 'home';
  let Screen = SCREENS[name] || Home;
  if (name === 'home' && s.role === 'telefono' && s.paired) Screen = PhoneHome;
  return (
    <>
      <Screen key={`${name}:${s.route.id || ''}`} {...s.route} />
      {s.toast && <div class="toast" role="status" data-testid="toast"><Icon name="spunta" />{s.toast}</div>}
      <Screensaver />
      <Emergency />
      <PttBar />
      <CallOverlay />
    </>
  );
}
