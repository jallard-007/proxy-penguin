// Command proxy-penguin is the entry point for the proxy-penguin reverse proxy
// and dashboard. It reads config.json, starts the proxy and API servers, and
// gracefully shuts down on SIGINT/SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jallard-007/proxy-pengiun/api"
	"github.com/jallard-007/proxy-pengiun/auth"
	"github.com/jallard-007/proxy-pengiun/broker"
	"github.com/jallard-007/proxy-pengiun/frontend"
	"github.com/jallard-007/proxy-pengiun/httputils"
	"github.com/jallard-007/proxy-pengiun/model"
	"github.com/jallard-007/proxy-pengiun/proxy"
	"github.com/jallard-007/proxy-pengiun/storage"
)

// Config holds the top-level application configuration loaded from config.json.
type Config struct {
	Addr          string            `json:"addr"`
	DBPath        string            `json:"dbPath"`
	Routes        map[string]string `json:"routes"`
	DashboardHost string            `json:"dashboardHost"`
	ApiPassword   string            `json:"apiPassword"`
}

func initMux(routes map[string]string, mux httputils.Router) error {
	for h, r := range routes {
		u, err := url.Parse(r)
		if err != nil {
			return fmt.Errorf("cannot parse host %s: %w", r, err)
		}
		prox := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(u)
				pr.Out.Host = pr.In.Host
			},
		}
		mux.Handle(h+"/", prox)
	}
	return nil
}

func main() {
	cfg := loadConfig("config.json")

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("storage init: %v", err)
	}
	defer store.Close()

	b := broker.New()

	authMgr := auth.NewManager(cfg.ApiPassword, store)

	records := make(chan *model.RecordEvent, 1024)

	apiSrv := api.NewServer(store, b, authMgr)

	mux := http.NewServeMux()

	var handler http.Handler = mux

	var router httputils.Router = mux
	oldR := router
	router = httputils.RouteFunc(func(pattern string, handler http.Handler) {
		oldR.Handle(pattern, handler)
		log.Println("Registered endpoint:", pattern)
	})

	apiSrv.RegisterRoutes(cfg.DashboardHost, router)
	fileServer := http.FileServerFS(frontend.FS)
	router.Handle(fmt.Sprintf("GET %s/", cfg.DashboardHost), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		fmt.Println("req", r.URL.Path)
		fileServer.ServeHTTP(w, r)
	}))

	err = initMux(cfg.Routes, router)
	if err != nil {
		log.Fatalf("registering routes: %v", err)
	}

	handler = proxy.Wrap(records, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go authMgr.StartCleanup(ctx)

	var wg sync.WaitGroup

	// Record processor: store + publish each request event.
	wg.Go(func() {
		for evt := range records {
			rec := evt.Record
			if rec.ID > 0 {
				// Completion update for an existing record.
				if err := store.Update(rec); err != nil {
					log.Printf("storage update: %v", err)
					continue
				}
			} else {
				// New (pending) record.
				if err := store.Insert(rec); err != nil {
					log.Printf("storage insert: %v", err)
					if evt.IDReady != nil {
						close(evt.IDReady)
					}
					continue
				}
				if evt.IDReady != nil {
					close(evt.IDReady)
				}
			}
			b.Publish(evt)
		}
	})

	srvr := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	wg.Go(func() {
		log.Printf("proxy-penguin listening on %s", cfg.Addr)
		if err := srvr.ListenAndServe(); err != http.ErrServerClosed {
			log.Println("proxy server error:", err)
		}
		stop()
	})

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	srvr.Shutdown(shutdownCtx)

	close(records)

	wg.Wait()

	log.Println("Done")
}

func loadConfig(path string) Config {
	cfg := Config{
		Addr:   ":8080",
		DBPath: "proxy-penguin.db",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Warning: could not read %s: %v (using defaults)", path, err)
		return cfg
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("invalid config %s: %v", path, err)
	}

	return cfg
}
