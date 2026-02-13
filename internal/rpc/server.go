package rpc

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/store"
)

type Server struct {
	cfg *config.Config
	st  *status.Store
	cs  *store.ChainStore
}

func New(cfg *config.Config) *Server {
	return &Server{cfg: cfg, st: status.NewStore(cfg.StatusFile), cs: store.NewChainStore(cfg.NetworkDataDir())}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/v0/status", s.handleStatus)
	mux.HandleFunc("/rpc/v0/chain/head", s.handleHead)
	mux.HandleFunc("/rpc/v0/chain/message/", s.handleMessageLookup)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("curiocore_up 1\n"))
	})
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := s.st.Read()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	head, _ := s.cs.Head()
	out := map[string]any{"status": st, "head": head}
	writeJSON(w, out)
}

func (s *Server) handleHead(w http.ResponseWriter, _ *http.Request) {
	head, err := s.cs.Head()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, head)
}

func (s *Server) handleMessageLookup(w http.ResponseWriter, r *http.Request) {
	cid := filepath.Base(r.URL.Path)
	writeJSON(w, map[string]any{"cid": cid, "found": false, "note": "message index not yet implemented", "ts": time.Now().Format(time.RFC3339)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.MarshalIndent(v, "", "  ")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
