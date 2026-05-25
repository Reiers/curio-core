# Contract addresses

Curio Core resolves contract addresses based on the `--network` flag.

## PDPVerifier

The on-chain verifier for possession proofs.

| Network | Proxy address |
|---|---|
| Calibration | `0x85e366Cf9DD2c0aE37E963d9556F5f4718d6417C` |
| Mainnet | `0xBADd0B92C1c71d02E7d520f64c0876538fa2557F` |

The proxy is stable; the implementation behind it changes per release (current target
is v3.4.0). See [FilOzone/pdp#271](https://github.com/FilOzone/pdp/issues/271) for the
rollout schedule.

## USDFC

The ERC-20 token clients pay storage in.

| Network | Address |
|---|---|
| Calibration | `0xb3042734b608a1B16e9e86B374A3f3e389B4cDf0` |
| Mainnet | `0x80B98d3aa09ffff255c3ba4A241111Ff1262F045` |

## FilecoinPay V1

The payment-rail contract that holds USDFC deposits, manages rail rates, and dispatches
`settleRail` calls.

| Network | Address |
|---|---|
| Calibration | `0x09a0fDc2723fAd1A7b8e3e00eE5DF73841df55a0` |
| Mainnet | `0x23b1e018F08BB982348b15a86ee926eEBf7F4DAa` |

## FilecoinWarmStorageService (FWSS)

The operator of payment rails for warm-storage SPs. FWSS controls each rail's
payment rate; the SP only ever receives. Address varies per FWSS deployment and is
typically not hard-coded by SP-side code.

## Service Registry

The Filecoin Service Registry contract where SPs publish their endpoint URLs.

| Network | Address |
|---|---|
| Calibration | (TBD; see upstream curio docs) |
| Mainnet | (TBD; see upstream curio docs) |

Registration costs 5 FIL — see [Funding the wallet](/getting-started/funding) for the
gas/FIL planning.

## How curio-core uses these

Curio Core's `pdp/contract` package (vendored from upstream Curio + regenerated for
the current ABI version) holds Go bindings for all of the above. The bindings are
network-aware via `contract.Network` and `contract.USDFCAddressFor(network)` etc.

For Solidity sources, see:

- PDPVerifier: <https://github.com/FilOzone/pdp>
- FilecoinPay: <https://github.com/FilOzone/filecoin-pay>
- FilecoinWarmStorageService: <https://github.com/FilOzone/filecoin-services>
