// FEVM contract address resolution for the embedded Lantern state-block
// prefetcher (lantern#44 wiring).
//
// At startup we tell Lantern which EVM contracts to walk into the local
// blockstore cache on every head advance. Walking the contract storage
// trie ahead of time means a subsequent eth_call (PDPVerifier reads,
// FWSS reads, ServiceProviderRegistry reads, USDFC reads) hits the
// cache rather than falling back to the VMBridge with "block not found".
//
// This is the curio-core side of lantern#44. The walk itself + the
// retry-on-miss wrapper for the eth_call hot path live in Lantern
// (state/prefetch, rpc/handlers/evmexec_fetch.go); this file just
// resolves the right contract addresses for the current network.

package main

import (
	"github.com/filecoin-project/curio/pdp/contract"
)

// fevmPrefetchAddrsForNetwork returns the EVM contract addresses (as
// 0x-prefixed hex) whose state subtrees Lantern should keep warm in
// the local cache for the given network. The list is intentionally
// small + stable:
//
//   - PDPVerifier proxy (every dataset status poll reads from here).
//   - FWSS proxy (PDP-payments + service-config reads).
//   - ServiceProviderRegistry proxy (provider lookups, dashboards).
//   - USDFC token (FilecoinPay balance/allowance reads).
//
// Unknown / devnet networks return nil; the prefetcher is no-op when
// passed an empty list.
func fevmPrefetchAddrsForNetwork(network string) []string {
	n := contract.Network(network)
	if n != contract.NetworkCalibration && n != contract.NetworkMainnet {
		return nil
	}
	addrs := make([]string, 0, 4)
	cs := contract.ContractAddressesFor(n)
	if (cs.PDPVerifier != [20]byte{}) {
		addrs = append(addrs, cs.PDPVerifier.Hex())
	}
	if (cs.AllowedPublicRecordKeepers.FWSService != [20]byte{}) {
		addrs = append(addrs, cs.AllowedPublicRecordKeepers.FWSService.Hex())
	}
	if reg, err := contract.ServiceRegistryAddressFor(n); err == nil {
		addrs = append(addrs, reg.Hex())
	}
	if usdfc, err := contract.USDFCAddressFor(n); err == nil {
		addrs = append(addrs, usdfc.Hex())
	}
	return addrs
}

