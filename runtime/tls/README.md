# Spike mTLS certificates

Milestone 1 generates short-lived test certificates with:

```bash
./scripts/runtime/generate-spike-certs.sh out/runtime-spike/tls
```

The generated directory is smoke input and must not be committed. No
certificate or private key is stored in the Guest Runtime artifact. QEMU
injects `ca.crt`, `server.crt`, and `server.key` through fw_cfg when the Guest
starts. Smoke clients use `ca.crt`, `client.crt`, and `client.key`, with TLS
server name `sealbuild-runtime`.

Certificate generation is intentionally strict: an existing output directory,
an OpenSSL failure, or a signing failure stops the build. Plaintext BuildKit
TCP is not an alternative path.
