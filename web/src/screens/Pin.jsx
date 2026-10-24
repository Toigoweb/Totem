import { PinPad, pinResult } from '../components/PinPad.jsx';
import { S } from '../strings.js';
import { back, enterMaintenance, replace } from '../app.js';
import { api } from '../api.js';

export function Pin() {
  return (
    <PinPad
      title={S.pinTitle}
      onCancel={back}
      onSubmit={async (pin) => {
        const r = await api('/api/maint/login', { method: 'POST', body: { pin } });
        const res = pinResult(r);
        if (res.ok) {
          enterMaintenance(r.data.token);
          replace('maint');
        }
        return res;
      }}
    />
  );
}
