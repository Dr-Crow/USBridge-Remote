// Disposable CI fixture only. This file is never packaged as a client asset.
// Full-leaf DER SHA256 comes from the fixture's reviewed local certificate,
// never discovery. This is a distinct authentication mode from production TLS.
export const FIXTURE_AUTHENTICATION = 'fixture-pinned-leaf';
const PROTOCOL = 'usbridge-webtransport-v1';
const FIELDS = ['role','protocol','endpoint','origin','leaf_sha256','leaf_not_before_ms','leaf_not_after_ms','session_id','ticket','ticket_expires_at_ms','session_expires_at_ms','width','height','fps','public_client_commit','private_fixture_sha256'];
const fail = code => { const e=new Error(code);e.code=code;throw e; };
function exact(value, fields) {
  if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==fields.length||fields.some(k=>!Object.hasOwn(value,k)))fail('fixture_fields');
}
function loopback(value,path) {
  let u;try{u=new URL(value);}catch{fail('fixture_destination');}
  if(typeof value!=='string'||value.length>128||u.href!==value||u.protocol!=='https:'||u.hostname!=='127.0.0.1'||!u.port||u.username||u.password||u.hash||u.search||u.pathname!==path)fail('fixture_destination');
}
function origin(value) {if(typeof value!=='string')fail('fixture_origin');loopback(value+'/', '/');return value;}
function live(c,now) {
  const time=now();
  if(!Number.isSafeInteger(time)||time<0)fail('fixture_clock');
  if(!Number.isSafeInteger(c.leaf_not_before_ms)||!Number.isSafeInteger(c.leaf_not_after_ms)||c.leaf_not_before_ms>time||c.leaf_not_after_ms<=time||c.leaf_not_after_ms-c.leaf_not_before_ms<=0||c.leaf_not_after_ms-c.leaf_not_before_ms>3600000)fail('fixture_certificate_validity');
  if(!Number.isSafeInteger(c.ticket_expires_at_ms)||!Number.isSafeInteger(c.session_expires_at_ms)||c.ticket_expires_at_ms<=time||c.ticket_expires_at_ms-time>30000||c.session_expires_at_ms<=time||c.session_expires_at_ms-time>300000||c.ticket_expires_at_ms>c.session_expires_at_ms||c.session_expires_at_ms>c.leaf_not_after_ms)fail('fixture_grant_expiry');
}

// Harnesses must label their closed receipt with authentication and must not
// advertise this injected transport through production tls_mode:'trusted'.
export function makePinnedLeafTransport(config,{NativeTransport=globalThis.WebTransport,now=()=>Date.now(),pageOrigin=globalThis.location?.origin,expectedPublicClientCommit,expectedPrivateFixtureSHA256}={}) {
  exact(config,FIELDS);
  const c=Object.freeze({...config});
  if(c.role!=='webtransport-pinned-leaf-test-v1'||c.protocol!==PROTOCOL)fail('fixture_role');
  loopback(c.endpoint,'/webtransport');
  if(origin(c.origin)!==pageOrigin)fail('fixture_origin');
  if(typeof c.leaf_sha256!=='string'||!/^[a-f0-9]{64}$/.test(c.leaf_sha256))fail('fixture_certificate_hash');
  if(typeof c.public_client_commit!=='string'||!/^[a-f0-9]{40}$/.test(c.public_client_commit)||typeof c.private_fixture_sha256!=='string'||!/^[a-f0-9]{64}$/.test(c.private_fixture_sha256))fail('fixture_provenance');
  if(c.public_client_commit!==expectedPublicClientCommit||c.private_fixture_sha256!==expectedPrivateFixtureSHA256)fail('fixture_provenance_mismatch');
  if(typeof c.session_id!=='string'||!/^[A-Za-z0-9_-]{1,64}$/.test(c.session_id)||typeof c.ticket!=='string'||!/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(c.ticket))fail('fixture_grant_identity');
  if(c.width!==128||c.height!==72||c.fps!==30)fail('fixture_video_profile');
  live(c,now);
  if(typeof NativeTransport!=='function')fail('fixture_native_transport');
  const grant=Object.freeze(Object.fromEntries(['protocol','session_id','ticket','endpoint','origin','ticket_expires_at_ms','session_expires_at_ms','width','height','fps'].map(k=>[k,c[k]])));
  let used=false;
  class PinnedFixtureTransport {
    constructor(endpoint, options={congestionControl:'low-latency'}) {
      if(used)fail('fixture_transport_reused');
      exact(options,['congestionControl']);
      if(options.congestionControl!=='low-latency'||endpoint!==c.endpoint)fail('fixture_transport_scope');
      live(c,now);
      used=true;
      const value=Uint8Array.from(c.leaf_sha256.match(/../g),x=>parseInt(x,16));
      return new NativeTransport(c.endpoint,{congestionControl:'low-latency',allowPooling:false,serverCertificateHashes:[{algorithm:'sha-256',value}]});
    }
  }
  return Object.freeze({authentication:FIXTURE_AUTHENTICATION,Transport:PinnedFixtureTransport,grant});
}
