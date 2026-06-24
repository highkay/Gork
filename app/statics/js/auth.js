/* Gork — Auth module */
const ADMIN_API = '/admin/api';
const WEBUI_API = '/webui/api';

const _ENC = new TextEncoder(),
  _DEC = new TextDecoder();
const _SECRET = 'gork-admin-key';
const _XOR_P = 'enc:xor:',
  _AES_P = 'enc:v1:';

function _toB64(b) {
  let s = '';
  b.forEach((v) => (s += String.fromCharCode(v)));
  return btoa(s);
}
function _fromB64(s) {
  const d = atob(s),
    a = new Uint8Array(d.length);
  for (let i = 0; i < d.length; i++) a[i] = d.charCodeAt(i);
  return a;
}
function _xor(d, k) {
  const o = new Uint8Array(d.length);
  for (let i = 0; i < d.length; i++) o[i] = d[i] ^ k[i % k.length];
  return o;
}

async function _deriveKey(salt) {
  const km = await crypto.subtle.importKey(
    'raw',
    _ENC.encode(_SECRET),
    'PBKDF2',
    false,
    ['deriveKey'],
  );
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations: 100000, hash: 'SHA-256' },
    km,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

async function _encrypt(plain) {
  if (!plain) return '';
  if (!crypto?.subtle)
    return _XOR_P + _toB64(_xor(_ENC.encode(plain), _ENC.encode(_SECRET)));
  const salt = crypto.getRandomValues(new Uint8Array(16)),
    iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await _deriveKey(salt),
    ct = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv },
      key,
      _ENC.encode(plain),
    );
  return `${_AES_P}${_toB64(salt)}:${_toB64(iv)}:${_toB64(new Uint8Array(ct))}`;
}

async function _decrypt(s) {
  if (!s) return '';
  if (s.startsWith(_XOR_P))
    return _DEC.decode(
      _xor(_fromB64(s.slice(_XOR_P.length)), _ENC.encode(_SECRET)),
    );
  if (!s.startsWith(_AES_P) || !crypto?.subtle) return '';
  const p = s.split(':');
  if (p.length !== 5) return '';
  const key = await _deriveKey(_fromB64(p[2]));
  return _DEC.decode(
    await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: _fromB64(p[3]) },
      key,
      _fromB64(p[4]),
    ),
  );
}

/* Key store factory */
function _keyStore(k) {
  const modeKey = `${k}:mode`;
  const storageGet = (store, key) => {
    try {
      return store.getItem(key) || '';
    } catch {
      return '';
    }
  };
  const storageSet = (store, key, value) => {
    try {
      store.setItem(key, value);
    } catch {
      /* ignore unavailable storage */
    }
  };
  const storageRemove = (store, key) => {
    try {
      store.removeItem(key);
    } catch {
      /* ignore unavailable storage */
    }
  };
  const clearPersistent = () => {
    storageRemove(localStorage, k);
    storageRemove(localStorage, modeKey);
  };
  return {
    get: async () => {
      const sessionValue = storageGet(sessionStorage, k);
      const persistentValue = storageGet(localStorage, k);
      const s = sessionValue || persistentValue;
      if (!s) return '';
      try {
        const value = await _decrypt(s);
        if (!sessionValue && storageGet(localStorage, modeKey) !== 'persistent') {
          storageSet(sessionStorage, k, (await _encrypt(value)) || '');
          clearPersistent();
        }
        return value;
      } catch {
        storageRemove(sessionStorage, k);
        clearPersistent();
        return '';
      }
    },
    set: async (v, options = {}) => {
      if (!v) {
        storageRemove(sessionStorage, k);
        clearPersistent();
        return;
      }
      const encrypted = (await _encrypt(v)) || '';
      if (options.remember === true) {
        storageRemove(sessionStorage, k);
        storageSet(localStorage, k, encrypted);
        storageSet(localStorage, modeKey, 'persistent');
        return;
      }
      storageSet(sessionStorage, k, encrypted);
      clearPersistent();
    },
    clear: () => {
      storageRemove(sessionStorage, k);
      clearPersistent();
    },
  };
}

const adminKey = _keyStore('gork_admin_key');
const webuiKey = _keyStore('gork_webui_key');
window.adminKey = adminKey;
window.webuiKey = webuiKey;

async function verifyKey(url, key) {
  return (
    await fetch(url, { headers: key ? { Authorization: `Bearer ${key}` } : {} })
  ).ok;
}

function adminLogout() {
  adminKey.clear();
  webuiKey.clear();
  location.href = '/admin/login';
}
function webuiLogout() {
  webuiKey.clear();
  location.href = '/webui/login';
}
