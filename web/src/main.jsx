import { render } from 'preact';

// Segnaposto: l'interfaccia si implementa dopo l'approvazione dei wireframe.
function App() {
  return (
    <main style={{ fontFamily: 'sans-serif', padding: '2rem' }}>
      <h1>Totem documenti di sicurezza</h1>
      <p>Versione {__APP_VERSION__}. Interfaccia in preparazione.</p>
    </main>
  );
}

render(<App />, document.getElementById('app'));
