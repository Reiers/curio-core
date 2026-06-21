package usdfcacquire

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestParseBigOpt(t *testing.T) {
	cases := []struct {
		in   string
		want *big.Int
	}{
		{"", nil},
		{"0", big.NewInt(0)},
		{"1000000", big.NewInt(1000000)},
		{"0x10", big.NewInt(16)},
		{"  42 ", big.NewInt(42)},
		{"notanumber", nil},
	}
	for _, c := range cases {
		got := parseBigOpt(c.in)
		if c.want == nil {
			if got != nil {
				t.Errorf("parseBigOpt(%q) = %v, want nil", c.in, got)
			}
			continue
		}
		if got == nil || got.Cmp(c.want) != 0 {
			t.Errorf("parseBigOpt(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseUint(t *testing.T) {
	if parseUint("500000") != 500000 {
		t.Error("decimal gasLimit")
	}
	if parseUint("0x7a120") != 500000 {
		t.Error("hex gasLimit")
	}
	if parseUint("") != 0 {
		t.Error("empty -> 0")
	}
}

func TestToECDSA_DerivesAddress(t *testing.T) {
	// Deterministic test key (NOT a real wallet).
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := crypto.FromECDSA(k) // 32 bytes
	got, err := toECDSA(raw)
	if err != nil {
		t.Fatalf("toECDSA: %v", err)
	}
	if crypto.PubkeyToAddress(got.PublicKey) != crypto.PubkeyToAddress(k.PublicKey) {
		t.Error("round-trip address mismatch")
	}
}

func TestToECDSA_RejectsBadLength(t *testing.T) {
	if _, err := toECDSA([]byte{1, 2, 3}); err == nil {
		t.Error("want error on short key")
	}
}

func TestSignAndBroadcast_RejectsBadRouteType(t *testing.T) {
	_, err := SignAndBroadcast(nil, SourceChain{ChainID: 1}, make([]byte, 32),
		TransactionRequest{RouteType: "DEPOSIT_ADDRESS", Target: "0xabc", Data: "0x01"})
	if err == nil {
		t.Error("want error for non-ON_CHAIN_EXECUTION route type")
	}
}
