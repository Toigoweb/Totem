import { render } from 'preact';
import '@fontsource/barlow/400.css';
import '@fontsource/barlow/500.css';
import '@fontsource/barlow/600.css';
import '@fontsource/barlow/700.css';
import '@fontsource/barlow-condensed/500.css';
import '@fontsource/barlow-condensed/600.css';
import '@fontsource/barlow-condensed/700.css';
import './styles.css';
import { App } from './App.jsx';
import { init } from './app.js';
import { installErrorHandlers } from './kiosk/errors.js';
import { installIdle } from './kiosk/idle.js';
import { installUpdates } from './kiosk/updates.js';

installErrorHandlers();
render(<App />, document.getElementById('app'));
init().then(() => {
  installIdle();
  installUpdates();
});
