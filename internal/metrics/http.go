// Package metrics — HTTP /metrics endpoint (Prometheus).
package metrics

import (
	"azcontainer/internal/state"
	"fmt"
	"net/http"
	"strings"
)

// ServeMetrics — HTTP server-i background-da işə salır.
//
// Daemon bu funksiyanı goroutine-də çağırır.
// Default port: 9090 (Prometheus standartı).
func ServeMetrics(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleMetrics)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleRoot)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return server.ListenAndServe()
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	store, err := state.NewStore()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	containers, err := store.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var sb strings.Builder

	// Help/Type headers.
	sb.WriteString("# HELP azcontainer_containers_total Bütün container sayı.\n")
	sb.WriteString("# TYPE azcontainer_containers_total gauge\n")
	fmt.Fprintf(&sb, "azcontainer_containers_total %d\n", len(containers))

	// Running containers count.
	running := 0
	for _, c := range containers {
		if c.Status == state.StatusRunning && state.IsAlive(c.PID) {
			running++
		}
	}
	sb.WriteString("# HELP azcontainer_containers_running İşləyən container sayı.\n")
	sb.WriteString("# TYPE azcontainer_containers_running gauge\n")
	fmt.Fprintf(&sb, "azcontainer_containers_running %d\n", running)

	// Per-container metrics.
	sb.WriteString("# HELP azcontainer_cpu_usage_nanoseconds CPU vaxtı (ns).\n")
	sb.WriteString("# TYPE azcontainer_cpu_usage_nanoseconds counter\n")

	for _, c := range containers {
		if c.Status != state.StatusRunning {
			continue
		}
		snap, err := Collect(c.ID)
		if err != nil {
			continue
		}
		sb.WriteString(snap.FormatPrometheus())
	}

	w.Write([]byte(sb.String()))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok\n"))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`azcontainer daemon
Endpoints:
  /metrics  Prometheus metrics
  /health   Health check
`))
}
