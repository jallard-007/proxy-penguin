// Command proxy-penguin is the entry point for the proxy-penguin reverse proxy
// and dashboard. It reads config.json, starts the proxy and API servers, and
// gracefully shuts down on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jallard-007/proxy-pengiun/backend/api"
	"github.com/jallard-007/proxy-pengiun/backend/auth"
	"github.com/jallard-007/proxy-pengiun/backend/event"
	"github.com/jallard-007/proxy-pengiun/backend/httputils"
	"github.com/jallard-007/proxy-pengiun/backend/model"
	"github.com/jallard-007/proxy-pengiun/backend/storage"
	"github.com/jallard-007/proxy-pengiun/frontend"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(realMain())
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

func realMain() int {
	configFile := "config.json"

	var cfg Config

	cmd := cobra.Command{
		Use: "proxy-penguin",
	}

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = loadConfig(configFile)
		if err != nil {
			return err
		}

		return nil
	}

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		// initialize DB
		store, err := storage.New(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("storage init: %w", err)
		}
		defer store.Close()

		// authentication manager
		authMgr := auth.NewManager(cfg.ApiPassword, store)

		apiSrv := api.NewServer(store)

		mux := http.NewServeMux()

		// log all endpoints that are registered
		router := httputils.RouteFunc(func(pattern string, handler http.Handler) {
			mux.Handle(pattern, handler)
			log.Println("Registered endpoint:", pattern)
		})

		apiSrv.RegisterRoutes(cfg.DashboardHost, router, authMgr)
		router.Handle(fmt.Sprintf("GET %s/", cfg.DashboardHost), http.FileServerFS(frontend.FS))

		err = initMux(cfg.Routes, router)
		if err != nil {
			return fmt.Errorf("registering routes: %w", err)
		}

		// wrap all requests with the proxy handler
		events := make(chan model.RecordEvent, 1024)
		maxId, err := store.MaxRowId(ctx)
		if err != nil {
			return fmt.Errorf("error getting max row id: %w", err)
		}

		var counter, missed atomic.Int64
		counter.Store(maxId + 1)

		var httpHandler http.Handler = mux
		// emit incoming events to events chan
		httpHandler = event.EmitEvents(&counter, &missed, events, httpHandler)
		// log events to logger for debugging purposes
		httpHandler = httputils.LogEvents(httpHandler)

		ctx, cancel := context.WithCancel(ctx)

		var wg sync.WaitGroup

		wg.Go(func() {
			authMgr.StartCleanup(ctx)
		})

		sseEvents := make(chan []model.RecordEvent, 1024)
		wg.Go(func() {
			apiSrv.RunSSE(ctx, sseEvents)
		})

		// Record processor: store + publish each request event.
		wg.Go(func() {
			defer close(sseEvents)
			event.HandleEvents(store, events, func(batch []model.RecordEvent) {
				if len(batch) > 0 {
					select {
					case sseEvents <- batch:
					default:
					}
				}
			})
		})

		srvr := &http.Server{
			Addr:    cfg.Addr,
			Handler: httpHandler,
			BaseContext: func(l net.Listener) context.Context {
				return ctx
			},
		}

		wg.Go(func() {
			log.Printf("proxy-penguin listening on %s", cfg.Addr)
			if err := srvr.ListenAndServe(); err != http.ErrServerClosed {
				log.Println("proxy server error:", err)
			}
			cancel()
		})

		wg.Go(func() {
			var prevMissed int64
			t := time.NewTicker(1 * time.Minute)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m := missed.Load()
				if m != prevMissed {
					log.Println("missed", m-prevMissed, "requests")
					prevMissed = m
				}
			}
		})

		<-ctx.Done()
		log.Println("Shutting down...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		srvr.Shutdown(shutdownCtx)

		close(events)

		wg.Wait()

		log.Println("Done")
		return nil
	}

	cmd.Flags().StringVar(&configFile, "config-file", "config.json", "")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := cmd.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}
