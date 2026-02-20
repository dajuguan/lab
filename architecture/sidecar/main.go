package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

const coreRPCURL = "http://127.0.0.1:8080" // Sidecar points to core via a named RPC endpoint.

// ---- Core Service ----
func startCore() {
	mux := http.NewServeMux()
	mux.HandleFunc("/work", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "core",
			"ts":      time.Now().Unix(),
			"status":  "ok",
		})
	})
	log.Println("core listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ---- Sidecar ----
// Independent module/process: it talks to core through coreRPCURL
// and provides extra capabilities (monitoring/metrics).
func startSidecar(coreRPCURL string) {
	var success int64
	var fail int64

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		client := &http.Client{Timeout: 1 * time.Second}

		for range ticker.C {
			resp, err := client.Get(coreRPCURL + "/work")
			if err != nil {
				atomic.AddInt64(&fail, 1)
				continue
			}
			resp.Body.Close()
			atomic.AddInt64(&success, 1)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"core_rpc_url":              coreRPCURL,
			"sidecar_core_call_success": atomic.LoadInt64(&success),
			"sidecar_core_call_fail":    atomic.LoadInt64(&fail),
		})
	})

	log.Println("sidecar listening on :9090")
	log.Fatal(http.ListenAndServe(":9090", mux))
}

func main() {
	go startCore()           // Core service (separate process in real deployments).
	startSidecar(coreRPCURL) // Sidecar explicitly depends on core RPC endpoint.
}
