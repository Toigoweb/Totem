import { render } from 'preact';
import './fonts.css';
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
