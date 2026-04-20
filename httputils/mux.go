// Package httputils provides helpers for composing HTTP routers.
package httputils

import (
	"net/http"
)

// Router is a minimal interface satisfied by *http.ServeMux and compatible muxes.
type Router interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// RouterFunc is a function that implements Router by treating itself as the
// Handle method and delegating HandleFunc through it.
type RouterFunc func(pattern string, handler http.Handler)

func (f RouterFunc) Handle(pattern string, handler http.Handler) {
	f(pattern, handler)
}

func (f RouterFunc) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	f.Handle(pattern, http.HandlerFunc(handler))
}

// RouteFunc wraps a single handler-registration function as a Router.
func RouteFunc(f func(pattern string, handler http.Handler)) Router {
	return RouterFunc(f)
}
