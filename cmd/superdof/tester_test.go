package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTesterAPI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "directoutputconfig11.ini"), "[version]\nmindofversion=0.8\n[Colors DOF]\nRed=#FF0000FF\n[Config DOF]\nsmoke,E120 150 I48,E101 Red\n")
	writeFile(t, filepath.Join(dir, "FX", "Smoke.FX"), "[STARTUP]\nFF_ROM=smoke\n[COMMANDS]\nBUMPER01|ON|FF_DOF E120,-1|FF_Dev DV_LF,300\n")
	writeFile(t, filepath.Join(dir, "DOFLinx.INI"), "PATH_FX="+filepath.ToSlash(filepath.Join(dir, "FX"))+"\nLINK_LF=1017,50,5000,255\nLINK_LX=2001\n")

	tr := &tester{log: &ringLog{}}
	mux := http.NewServeMux()
	tr.routes(mux)
	srv := httptest.NewServer(guard(mux))
	defer srv.Close()
	defer tr.unload()

	call := func(path string, body any) map[string]any {
		t.Helper()
		var rd *bytes.Reader
		method := http.MethodGet
		if body != nil {
			b, _ := json.Marshal(body)
			rd, method = bytes.NewReader(b), http.MethodPost
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		json.NewDecoder(resp.Body).Decode(&out)
		out["_status"] = float64(resp.StatusCode)
		return out
	}

	if r := call("/api/output", map[string]any{"controller": "x"}); r["_status"] != float64(409) {
		t.Fatalf("expected 409 before load, got %v", r)
	}
	r := call("/api/load", map[string]any{"ini": filepath.Join(dir, "DOFLinx.INI"), "iniDir": dir, "autoCabinet": true, "dry": true})
	if r["error"] != nil {
		t.Fatal(r["error"])
	}
	info := call("/api/info", nil)
	if info["dry"] != true || info["romCount"] != float64(1) {
		t.Fatalf("info %v", info)
	}
	links := info["links"].([]any)
	if len(links) != 2 {
		t.Fatalf("links %v", links)
	}
	if st := links[1].(map[string]any)["status"]; st == "ok" {
		t.Fatalf("device 2 must be flagged as unreachable, got %v", st)
	}

	if r := call("/api/output", map[string]any{"controller": "PinOne Controller 01", "number": 5, "value": 200}); r["error"] != nil {
		t.Fatal(r["error"])
	}
	live := call("/api/live?since=0", nil)
	pin := live["controllers"].([]any)[0].(map[string]any)["values"].([]any)
	if pin[4] != float64(200) {
		t.Fatalf("output 5 should be 200, got %v", pin[4])
	}
	call("/api/release", map[string]any{})

	rom := call("/api/rom", map[string]any{"rom": "smoke"})
	els := rom["elements"].([]any)
	if rom["found"] != true || len(els) != 2 {
		t.Fatalf("rom %v", rom)
	}
	call("/api/element", map[string]any{"desc": "E101", "mode": "on"})
	deadline := time.Now().Add(2 * time.Second)
	for {
		live = call("/api/live?since=0", nil)
		pin = live["controllers"].([]any)[0].(map[string]any)["values"].([]any)
		sum := 0.0
		for _, v := range pin {
			sum += v.(float64)
		}
		if sum > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("firing E101 did not light any output")
		}
		time.Sleep(20 * time.Millisecond)
	}

	g := call("/api/game", map[string]any{"kind": "FX", "table": "Smoke"})
	if g["error"] != nil || len(g["events"].([]any)) == 0 {
		t.Fatalf("game %v", g)
	}
	if r := call("/api/linx", map[string]any{"mode": "trigger", "text": "E,BUMPER01,1"}); r["error"] != nil {
		t.Fatal(r["error"])
	}
	if r := call("/api/alloff", map[string]any{}); r["error"] != nil {
		t.Fatal(r["error"])
	}
}

func TestGuardRejectsForeignHostAndOrigin(t *testing.T) {
	h := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct{ host, origin, ct string }{
		{"evil.example", "", ""},
		{"127.0.0.1:1", "http://evil.example", "application/json"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/alloff", nil)
		req.Host = tc.host
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		req.Header.Set("Content-Type", tc.ct)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("%+v: got %d", tc, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Host = "127.0.0.1:5"
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("non-JSON POST must be rejected, got %d", rec.Code)
	}
}
