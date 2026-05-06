import {
  generateKeyPairSync,
  createPublicKey,
  createPrivateKey,
  randomBytes,
} from "crypto";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "fs";
import { dirname } from "path";

export interface SSHKeyPair {
  publicKey: string; // OpenSSH format
  privateKey: string; // OpenSSH format (ssh2-compatible)
}

/** Write a uint32 big-endian into a Buffer */
function writeU32(value: number): Buffer {
  const buf = Buffer.alloc(4);
  buf.writeUInt32BE(value);
  return buf;
}

/** Build an SSH "string" (4-byte length prefix + data) */
function sshString(data: string | Buffer): Buffer {
  const buf = typeof data === "string" ? Buffer.from(data) : data;
  return Buffer.concat([writeU32(buf.length), buf]);
}

/**
 * Convert an Ed25519 private key (Node crypto KeyObject) to OpenSSH format.
 * ssh2 cannot parse PKCS#8 Ed25519 keys, so we must produce the
 * openssh-key-v1 binary format.
 */
function toOpenSSHPrivateKey(
  privateKeyPem: string,
  publicKeyBytes: Buffer,
  comment: string
): string {
  // Extract the 32-byte Ed25519 seed from PKCS#8 DER
  const keyObj = createPrivateKey(privateKeyPem);
  const pkcs8Der = keyObj.export({ type: "pkcs8", format: "der" });
  // PKCS#8 DER for Ed25519: fixed 16-byte header then 32-byte seed
  const seed = pkcs8Der.subarray(pkcs8Der.length - 32);

  // Ed25519 "private key" in OpenSSH = seed (32) || public (32) = 64 bytes
  const ed25519PrivBlob = Buffer.concat([seed, publicKeyBytes]);

  // Public key blob (same format as the public key line)
  const pubBlob = Buffer.concat([
    sshString("ssh-ed25519"),
    sshString(publicKeyBytes),
  ]);

  // Check integers (must match so decryption is verified as correct)
  const check = randomBytes(4);
  const privSection = Buffer.concat([
    check,
    check,
    sshString("ssh-ed25519"),
    sshString(publicKeyBytes),
    sshString(ed25519PrivBlob),
    sshString(comment),
  ]);

  // Pad to 8-byte block boundary (cipher block size for "none")
  const blockSize = 8;
  const padLen = blockSize - (privSection.length % blockSize);
  const padding =
    padLen < blockSize
      ? Buffer.from(Array.from({ length: padLen }, (_, i) => i + 1))
      : Buffer.alloc(0);
  const paddedPriv = Buffer.concat([privSection, padding]);

  // Assemble the full openssh-key-v1 blob
  const magic = Buffer.from("openssh-key-v1\0");
  const blob = Buffer.concat([
    magic,
    sshString("none"), // cipher
    sshString("none"), // kdf
    sshString(Buffer.alloc(0)), // kdf options
    writeU32(1), // number of keys
    sshString(pubBlob),
    sshString(paddedPriv),
  ]);

  // PEM-encode with 70-char lines
  const b64 = blob.toString("base64");
  const lines: string[] = [];
  for (let i = 0; i < b64.length; i += 70) {
    lines.push(b64.slice(i, i + 70));
  }

  return [
    "-----BEGIN OPENSSH PRIVATE KEY-----",
    ...lines,
    "-----END OPENSSH PRIVATE KEY-----",
    "", // trailing newline
  ].join("\n");
}

/**
 * Generate an Ed25519 SSH key pair.
 * Returns the key pair in the formats needed for SSH and the ServerPilot API.
 */
export function generateSSHKeyPair(comment: string): SSHKeyPair {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519", {
    publicKeyEncoding: {
      type: "spki",
      format: "pem",
    },
    privateKeyEncoding: {
      type: "pkcs8",
      format: "pem",
    },
  });

  // Extract 32-byte Ed25519 public key from SPKI DER
  const pubKeyObj = createPublicKey(publicKey);
  const derBuffer = pubKeyObj.export({ type: "spki", format: "der" });
  // Ed25519 SPKI DER has a fixed 12-byte header, key data is the remaining 32 bytes
  const keyData = derBuffer.subarray(12);

  // Build OpenSSH public key line
  const opensshPubKey = Buffer.concat([
    sshString("ssh-ed25519"),
    sshString(keyData),
  ]).toString("base64");

  // Convert private key to OpenSSH format (ssh2-compatible)
  const opensshPrivateKey = toOpenSSHPrivateKey(privateKey, keyData, comment);

  return {
    publicKey: `ssh-ed25519 ${opensshPubKey} ${comment}`,
    privateKey: opensshPrivateKey,
  };
}

/**
 * Load an existing key pair from disk, or generate and save a new one.
 */
export function ensureSSHKeyPair(
  keyPath: string,
  keyName: string
): SSHKeyPair {
  const privatePath = keyPath;
  const publicPath = `${keyPath}.pub`;

  if (existsSync(privatePath) && existsSync(publicPath)) {
    return {
      privateKey: readFileSync(privatePath, "utf-8"),
      publicKey: readFileSync(publicPath, "utf-8").trim(),
    };
  }

  // Ensure directory exists
  const dir = dirname(keyPath);
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true, mode: 0o700 });
  }

  const keyPair = generateSSHKeyPair(keyName);

  writeFileSync(privatePath, keyPair.privateKey, { mode: 0o600 });
  writeFileSync(publicPath, keyPair.publicKey + "\n", { mode: 0o644 });

  return keyPair;
}

/**
 * Check if a key pair exists on disk.
 */
export function sshKeyExists(keyPath: string): boolean {
  return existsSync(keyPath) && existsSync(`${keyPath}.pub`);
}
