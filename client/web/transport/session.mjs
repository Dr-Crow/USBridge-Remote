import {discovery} from './policy.mjs';
import {ProtocolError} from './wire.mjs';
import {ownAuthenticatedControl,ownAuthenticatedVideo} from './session_owner.mjs';

// Production entrypoints: capability discovery can only confirm the already
// paired endpoint using ordinary trusted TLS. It cannot choose a certificate
// pin or a different destination. Fixture authentication never enters here.
function authorize(options) {
  const candidate=discovery(options.capabilityStatus,options.capabilityBody,options.authorizedEndpoint);
  if(!candidate.available)throw new ProtocolError(candidate.reason);
  return options;
}
export async function negotiateControl(options,dependencies={}) {
  return ownAuthenticatedControl(authorize(options),dependencies);
}
export async function negotiateVideo(options,dependencies={}) {
  return ownAuthenticatedVideo(authorize(options),dependencies);
}
