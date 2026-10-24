// Stato dell'app: un piccolo store osservabile, senza librerie.
import { useEffect, useState } from 'preact/hooks';

export function createStore(initial) {
  let state = initial;
  const listeners = new Set();
  return {
    get: () => state,
    set(patch) {
      const p = typeof patch === 'function' ? patch(state) : patch;
      state = { ...state, ...p };
      for (const l of listeners) l(state);
    },
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
}

export function useStore(store, select = (s) => s) {
  const [v, setV] = useState(() => select(store.get()));
  useEffect(() => {
    setV(select(store.get()));
    return store.subscribe((s) => setV(select(s)));
  }, [store]);
  return v;
}
