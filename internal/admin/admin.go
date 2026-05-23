// Package admin exposes small operator/test endpoints that aren't part
// of the upstream curio/pdp HTTP API but are useful for end-to-end
// validation of the curio-core embedded Lantern + SenderETH path.
//
// Routes mount under /admin/* (no auth today; intended for loopback
// access only). The reverse-proxy in front of curio-core should NOT
// forward /admin/* to the public internet.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"github.com/curiostorage/harmonyquery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/go-chi/chi/v5"

	"github.com/Reiers/curio-core/internal/ethkeys"
)

// Routes mounts /admin/* onto r.
func Routes(r *chi.Mux, db harmonyquery.DBInterface, sender *message.SenderETH) {
	r.Route("/admin", func(r chi.Router) {
		r.Post("/test-tx", testTxHandler(db, sender))
		r.Get("/eth-key", ethKeyHandler(db))
	})
}

// testTxHandler triggers a 1-wei self-transfer through SenderETH so
// operators can verify the embedded-Lantern signing + broadcast path
// without driving the full PDP HTTP flow.
//
// Body (optional, all fields default to a 1-wei self-transfer):
//
//	{"to": "0x...", "value": "0x1", "data": "0x"}
//
// Response: { "txHash": "0x...", "from": "0x..." }
func testTxHandler(db harmonyquery.DBInterface, sender *message.SenderETH) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if sender == nil {
			http.Error(w, "SenderETH not wired (lantern disabled?)", http.StatusServiceUnavailable)
			return
		}
		ctx := r.Context()

		fromHex, err := ethkeys.LookupPDP(ctx, db)
		if err != nil {
			http.Error(w, "lookup pdp key: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if fromHex == "" {
			http.Error(w, "no eth_keys row with role='pdp'", http.StatusPreconditionFailed)
			return
		}
		from := common.HexToAddress(fromHex)

		// Defaults: self-transfer 1 wei, no data.
		toAddr := from
		value := big.NewInt(1)
		var data []byte

		// Decode optional overrides.
		var body struct {
			To    string `json:"to,omitempty"`
			Value string `json:"value,omitempty"`
			Data  string `json:"data,omitempty"`
		}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				if body.To != "" {
					toAddr = common.HexToAddress(body.To)
				}
				if body.Value != "" {
					vStr := body.Value
					if len(vStr) >= 2 && (vStr[:2] == "0x" || vStr[:2] == "0X") {
						vStr = vStr[2:]
					}
					v, ok := new(big.Int).SetString(vStr, 16)
					if !ok {
						http.Error(w, "bad value hex", http.StatusBadRequest)
						return
					}
					value = v
				}
				if body.Data != "" {
					dataStr := body.Data
					if len(dataStr) >= 2 && (dataStr[:2] == "0x" || dataStr[:2] == "0X") {
						dataStr = dataStr[2:]
					}
					data, err = hexDecode(dataStr)
					if err != nil {
						http.Error(w, "bad data hex: "+err.Error(), http.StatusBadRequest)
						return
					}
				}
			}
		}

		tx := types.NewTransaction(0, toAddr, value, 0, nil, data)

		txHash, err := sender.Send(asBackground(ctx), from, tx, "admin-test-tx")
		if err != nil {
			http.Error(w, "sender.Send: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"txHash": txHash.Hex(),
			"from":   from.Hex(),
		})
	}
}

// ethKeyHandler returns the currently active eth_keys row (read-only,
// without leaking the private key bytes).
func ethKeyHandler(db harmonyquery.DBInterface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		addr, err := ethkeys.LookupPDP(r.Context(), db)
		if err != nil {
			http.Error(w, "lookup pdp key: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"address": addr, "role": "pdp"})
	}
}

// hexDecode parses a hex string, no 0x prefix. Returns an empty byte
// slice for empty input.
func hexDecode(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var hi, lo byte
		if err := hexNibble(s[2*i], &hi); err != nil {
			return nil, err
		}
		if err := hexNibble(s[2*i+1], &lo); err != nil {
			return nil, err
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte, out *byte) error {
	switch {
	case '0' <= c && c <= '9':
		*out = c - '0'
	case 'a' <= c && c <= 'f':
		*out = 10 + c - 'a'
	case 'A' <= c && c <= 'F':
		*out = 10 + c - 'A'
	default:
		return fmt.Errorf("bad hex char %q", c)
	}
	return nil
}

// asBackground returns a context that's NOT cancelled when the request
// finishes (SenderETH inserts into message_sends_eth and the harmonytask
// processes it asynchronously; cancelling at HTTP-response time would
// abort mid-broadcast).
func asBackground(_ context.Context) context.Context {
	return context.Background()
}
