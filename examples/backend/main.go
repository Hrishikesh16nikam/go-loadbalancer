// Command backend is a tiny HTTP server used to demo and benchmark the load
// balancer. Run several on different ports; give one a -delay to see how
// least_connections routes around a slow server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", ":9001", "listen address")
	name := flag.String("name", "backend", "name included in every response")
	delay := flag.Duration("delay", 0, "artificial latency added to every request, e.g. 200ms")
	flag.Parse()

	var healthy atomic.Bool
	healthy.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	// Flip health on and off to watch the load balancer react.
	mux.HandleFunc("/toggle", func(w http.ResponseWriter, r *http.Request) {
		v := !healthy.Load()
		healthy.Store(v)
		fmt.Fprintf(w, "%s healthy=%v\n", *name, v)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if *delay > 0 {
			time.Sleep(*delay)
		}
		fmt.Fprintf(w, "hello from %s\n", *name)
	})

	log.Printf("%s listening on %s (delay %s)", *name, *addr, *delay)
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
