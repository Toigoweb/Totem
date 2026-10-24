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

const SCREENS = { home: Home, search: Search, section: Section, reader: Reader, status: Status, pin: Pin, maint: Maint, form: MaintForm, changePin: ChangePin, wizard: Wizard, devices: Devices, pairStation: PairStation, problems: Problems };
const MAINT_ONLY = new Set(['maint', 'form', 'changePin', 'devices', 'pairStation', 'problems']);

export function App() {
  const s = useStore(app);
  useEffect(() => {
    document.documentElement.dataset.scale = s.scale;
  }, [s.scale]);
  if (!s.ready) return null;
  let name = s.route.name;
  if (MAINT_ONLY.has(name) && !s.maint) name = 'home'; // manutenzione scaduta
  const Screen = SCREENS[name] || Home;
  return (
    <>
      <Screen key={`${name}:${s.route.id || ''}`} {...s.route} />
      {s.toast && <div class="toast" role="status" data-testid="toast"><Icon name="spunta" />{s.toast}</div>}
    </>
  );
}
