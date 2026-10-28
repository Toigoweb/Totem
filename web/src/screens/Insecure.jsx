import { Icon } from '../components/Icon.jsx';
import { S } from '../strings.js';

/**
 * Il browser offre cache offline, service worker e crittografia solo in HTTPS (o su
 * localhost). Aperto in http://indirizzo:porta il totem non può funzionare: lo si dice
 * chiaramente invece di mostrare una pagina bianca.
 */
export function missingSecureFeatures() {
  return !window.isSecureContext || !('caches' in window) || !('serviceWorker' in navigator) || !(window.crypto && crypto.subtle);
}

export function Insecure() {
  const host = location.host;
  return (
    <div class="screen" data-testid="insecure">
      <header class="topbar"><div class="sx" /><h1><span class="t">{S.appTitle}</span></h1><div class="dx" /></header>
      <div class="body">
        <div class="vuoto"><div class="c">
          <Icon name="lucchetto" class="grande" />
          <h2>{S.insecureTitle}</h2>
          <p>{S.insecureText(host)}</p>
          <p class="piccolo">{S.insecureHelp}</p>
        </div></div>
      </div>
    </div>
  );
}
