/* Source-browser request boundaries. This is not an OS network firewall. */
(() => {
  'use strict';
  const config = globalThis.USBridgeRuntimeConfig;
  if (config && config.strictLAN === false) return;
  const local = input => {
    const u = new URL(input, globalThis.location.href);
    if (u.username || u.password || u.hash) throw new Error('Strict LAN: credentials/fragments forbidden');
    const h = u.hostname.replace(/^\[|\]$/g, '').toLowerCase();
    const ipv4 = /^\d+\.\d+\.\d+\.\d+$/.test(h) ? h.split('.').map(Number) : null;
    const loop = h === '::1' || (ipv4 && ipv4[0] === 127);
    const private4 = ipv4 && (ipv4[0] === 10 || (ipv4[0] === 172 && ipv4[1] >= 16 && ipv4[1] <= 31) || (ipv4[0] === 192 && ipv4[1] === 168) || (ipv4[0] === 169 && ipv4[1] === 254));
    const private6 = h.includes(':') && !h.includes('.') && (/^f[cd][0-9a-f]{2}:/.test(h) || /^fe[89ab][0-9a-f]:/.test(h));
    if (!(loop || private4 || private6)) throw new Error('Strict LAN: literal local IP required');
    if (!(u.protocol === 'https:' || u.protocol === 'wss:' || (loop && (u.protocol === 'http:' || u.protocol === 'ws:')))) throw new Error('Strict LAN: TLS required');
    return u.href;
  };
  const fetch = globalThis.fetch;
  globalThis.fetch = (input, options) => {
    try { local(typeof input === 'string' || input instanceof URL ? input : input.url); }
    catch (error) { return Promise.reject(error); }
    return fetch.call(globalThis, input, {...options, redirect: 'error'});
  };
  if (globalThis.WebSocket) {
    const WS = globalThis.WebSocket;
    globalThis.WebSocket = class extends WS { constructor(url, protocols) { super(local(url), protocols); } };
  }
  if (globalThis.RTCPeerConnection) {
    const RTC = globalThis.RTCPeerConnection;
    const check = c => { if (c && c.iceServers && c.iceServers.length) throw new Error('Strict LAN: ICE servers forbidden'); };
    globalThis.RTCPeerConnection = class extends RTC {
      constructor(c, constraints) { check(c); super({...c, iceServers: []}, constraints); }
      setConfiguration(c) { check(c); return super.setConfiguration({...c, iceServers: []}); }
    };
  }
  if (globalThis.open) {
    const open = globalThis.open;
    globalThis.open = (url, ...args) => open.call(globalThis, local(url), ...args);
  }
})();
