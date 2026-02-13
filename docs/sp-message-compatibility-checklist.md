# SP Message Compatibility Checklist (Next Alpha)

## Scope
Groundwork for `curiocore chain msg --decode` and follow-up SP-oriented message tooling.

## Checklist
- [ ] Decode supports hex input (`0x...` and raw hex).
- [ ] Decode supports base64 input.
- [ ] Output includes detected encoding and printable payload preview.
- [ ] `--explain` describes decode fallback path.
- [ ] Non-decodable input returns actionable error text.

## Command Spec
- `curiocore chain msg --decode <hex|base64> [--explain]`

### Example
```bash
curiocore chain msg --decode 0x68656c6c6f --explain
```

Expected staged UX:
1. Parse input + infer encoding.
2. Decode payload.
3. Print result and next-step hint.

## Acceptance Criteria (Measurable)
1. Valid hex input exits 0 and prints `Decoded (hex):`.
2. Valid base64 input exits 0 and prints `Decoded (base64):`.
3. Invalid input exits non-zero and includes `provide valid hex (0x...) or base64`.
4. `--explain` includes text `attempts hex decode first`.
