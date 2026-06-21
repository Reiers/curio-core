package usdfcacquire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoute_NoIntegratorID(t *testing.T) {
	c := NewClient("", "")
	if c.HasIntegratorID() {
		t.Fatal("empty id should not be 'has'")
	}
	_, err := c.Route(context.Background(), RouteParams{FromChain: "1", FromAmount: "1000000"})
	if !errors.Is(err, ErrNoIntegratorID) {
		t.Fatalf("want ErrNoIntegratorID, got %v", err)
	}
}

func TestRoute_BuildsRequestAndDecodes(t *testing.T) {
	var gotBody map[string]any
	var gotHdr string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr = r.Header.Get("x-integrator-id")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{
			"quoteId":"q123",
			"route":{
				"estimate":{"toAmount":"2980000000000000000","toAmountMin":"2920000000000000000","fromAmount":"3000000","exchangeRate":"0.993","aggregatePriceImpact":"0.2"},
				"transactionRequest":{"type":"ON_CHAIN_EXECUTION","target":"0xabc","data":"0xdeadbeef","value":"0","gasLimit":"500000"}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient("test-id", srv.URL)
	resp, err := c.Route(context.Background(), RouteParams{
		FromChain:   "1",
		FromToken:   "0xUSDC",
		FromAmount:  "3000000",
		FromAddress: "0xSender",
		ToAddress:   "0x5Df2",
		// ToToken/ToChain left blank -> defaults applied
	})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if gotHdr != "test-id" {
		t.Errorf("integrator header = %q", gotHdr)
	}
	if gotBody["toToken"] != USDFCFilecoin {
		t.Errorf("toToken default = %v, want %s", gotBody["toToken"], USDFCFilecoin)
	}
	if gotBody["toChain"] != FilecoinChainID {
		t.Errorf("toChain default = %v, want %s", gotBody["toChain"], FilecoinChainID)
	}
	if gotBody["slippage"].(float64) != defaultSlippage {
		t.Errorf("slippage default = %v", gotBody["slippage"])
	}
	if resp.QuoteID != "q123" {
		t.Errorf("quoteId = %q", resp.QuoteID)
	}
	tr := resp.Route.TransactionRequest
	if tr.Target != "0xabc" || tr.Data != "0xdeadbeef" || tr.GasLimit != "500000" {
		t.Errorf("transactionRequest not decoded: %+v", tr)
	}
	if resp.Route.Estimate.ToAmount != "2980000000000000000" {
		t.Errorf("estimate.toAmount = %q", resp.Route.Estimate.ToAmount)
	}
}

func TestRoute_401MapsToNoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Integrator ID is invalid","type":"UNAUTHORIZED"}`))
	}))
	defer srv.Close()
	c := NewClient("bad", srv.URL)
	_, err := c.Route(context.Background(), RouteParams{FromChain: "1", FromAmount: "1"})
	if !errors.Is(err, ErrNoIntegratorID) {
		t.Fatalf("401 should map to ErrNoIntegratorID, got %v", err)
	}
}

func TestStatus_Decodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transactionId") != "0xsrc" {
			t.Errorf("missing transactionId")
		}
		_, _ = w.Write([]byte(`{"status":"success","fromChain":{},"toChain":{}}`))
	}))
	defer srv.Close()
	c := NewClient("id", srv.URL)
	st, err := c.Status(context.Background(), "0xsrc", "1", "314", "q123")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "success" {
		t.Errorf("status = %q", st.Status)
	}
}
