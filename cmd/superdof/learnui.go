package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"superdof/internal/linx"
)

//go:embed learn.html
var learnHTML []byte

// serveLearnUI serves the game id assignment page on loopback. It returns a
// stop function; an empty addr disables it.
func serveLearnUI(addr string, mgr *linx.Manager, lg linx.Logger) func() {
	if addr == "" || addr == "off" {
		return func() {}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		lg.Warnf("game id page not available on %s: %v (use -ui to pick another port, -ui off to disable)", addr, err)
		return func() {}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(learnHTML)
	})
	mux.HandleFunc("/api/ids", func(w http.ResponseWriter, r *http.Request) {
		k, t := mgr.Current()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		tables := map[string][]string{}
		for _, kind := range mgr.ProfileKinds() {
			if l := mgr.TablesFor(kind); len(l) > 0 {
				tables[kind] = l
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"ids": mgr.SeenIDs(), "kind": k, "table": t, "tables": tables})
	})
	mux.HandleFunc("/api/assign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		var req struct{ ID, Table string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Table) == "" {
			http.Error(w, "id and table required", 400)
			return
		}
		mgr.Learn(req.ID, req.Table)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	srv := &http.Server{Handler: guard(mux), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	lg.Infof("game id page: http://%s/  (assign the ids the game sends to table files)", ln.Addr())
	return func() { srv.Close() }
}

var _ = fmt.Sprintf
