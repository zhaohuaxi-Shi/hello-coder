let cachedPublicKeyB64 = null;

function b64ToBytes(b64) {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function bytesToB64(buf) {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

/** Fetch and cache SPKI public key (base64) for RSA-OAEP. */
export async function getPublicKeyB64(api) {
  if (cachedPublicKeyB64) return cachedPublicKeyB64;
  const info = await api("/api/v1/crypto/public-key");
  if (!info?.publicKey) throw new Error("public key missing");
  cachedPublicKeyB64 = info.publicKey;
  return cachedPublicKeyB64;
}

/** Encrypt a password for transport. Empty string stays empty. */
export async function encryptSecret(plain, api) {
  if (!plain) return "";
  const publicKeyB64 = await getPublicKeyB64(api);
  const key = await crypto.subtle.importKey(
    "spki",
    b64ToBytes(publicKeyB64),
    { name: "RSA-OAEP", hash: "SHA-256" },
    false,
    ["encrypt"]
  );
  const cipher = await crypto.subtle.encrypt(
    { name: "RSA-OAEP" },
    key,
    new TextEncoder().encode(plain)
  );
  return bytesToB64(cipher);
}
