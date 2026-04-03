# History

## Description
- Record each Git change or local file change summary.
- Each entry includes reason, impact scope, and related links (put links in Notes when applicable).
- If owner is not specified, default to `<project-name>-agent-1`.
- Use datetime format `YYYY-MM-DD HH:MM` (24h).

## Mandatory Action
- MUST: When this table reaches 50 entries, compress the records into shorter and more general summaries, keeping stable and reusable change points.

## Record Template
| Date Time | Type | Summary | Reason | Impact Scope | Owner Id | Notes |
| ---- | ---- | ---- | ---- | ---- | ---- | ---- |
| 2026-04-03 15:59 | local change | Hardened VLESS encryption/decryption handling so Xray-side PQ handshake only enables when both panel fields are present, and added regression tests. | Prevent half-configured VLESS Encryption/Decryption from leaving node runtime in an inconsistent state while validating compatibility with panel-provided fields. | api/panel, core/xray | v2bx-agent-1 | Verified with `go test ./api/panel ./core/xray`. |
| 2026-04-03 17:09 | local change | Added runtime adaptation from panel-provided raw X25519 base64url keys to Xray-core `mlkem768x25519plus.native...` VLESS Encryption/Decryption strings. | Current xray-core build rejects raw base64 keys in inbound/outbound VLESS settings, so panel values must be transformed before building configs. | core/xray | v2bx-agent-1 | Verified with `go test ./api/panel ./core/xray`. |
|  |  |  |  |  |  |  |
