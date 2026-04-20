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
	"syscall"
	"time"

	"github.com/jallard-007/proxy-pengiun/api"
	"github.com/jallard-007/proxy-pengiun/auth"
	"github.com/jallard-007/proxy-pengiun/broker"
	"github.com/jallard-007/proxy-pengiun/event"
	"github.com/jallard-007/proxy-pengiun/frontend"
	"github.com/jallard-007/proxy-pengiun/httputils"
	"github.com/jallard-007/proxy-pengiun/model"
	"github.com/jallard-007/proxy-pengiun/storage"
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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig("config.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// initialize DB
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "storage init:", err)
		return 1
	}
	defer store.Close()

	// init event broker
	b := broker.New()

	// authentication manager
	authMgr := auth.NewManager(cfg.ApiPassword, store)

	apiSrv := api.NewServer(store, b, authMgr)

	mux := http.NewServeMux()

	var httpHandler http.Handler = mux

	var router httputils.Router = mux
	oldR := router
	router = httputils.RouteFunc(func(pattern string, handler http.Handler) {
		oldR.Handle(pattern, handler)
		log.Println("Registered endpoint:", pattern)
	})

	apiSrv.RegisterRoutes(cfg.DashboardHost, router)
	router.Handle(fmt.Sprintf("GET %s/", cfg.DashboardHost), http.FileServerFS(frontend.FS))

	err = initMux(cfg.Routes, router)
	if err != nil {
		fmt.Fprintln(os.Stderr, "registering routes:", err)
		return 1
	}

	// wrap all requests with the proxy handler
	events := make(chan model.RecordEvent, 1024)
	httpHandler = event.EmitEvents(events, httpHandler)

	go authMgr.StartCleanup(ctx)

	var wg sync.WaitGroup

	// Record processor: store + publish each request event.
	wg.Go(func() {
		event.Handle(events, store, b)
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
		stop()
	})

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	srvr.Shutdown(shutdownCtx)

	close(events)

	wg.Wait()

	log.Println("Done")
	return 0
}
