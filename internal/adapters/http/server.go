package http

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	api "timetrack/internal/adapters/http/gen"
	"timetrack/internal/core/service"
)

//go:embed webui
var webuiFS embed.FS

// NewServer baut den kompletten Handler: generierte API unter /api/*,
// eingebettete Weboberfläche unter /.
func NewServer(svc *service.Service, loc *time.Location) http.Handler {
	mux := http.NewServeMux()
	webui, err := fs.Sub(webuiFS, "webui")
	if err != nil {
		panic(err) // embed-Pfad ist zur Compile-Zeit fix
	}
	mux.Handle("/", http.FileServerFS(webui))
	return api.HandlerFromMux(&Handler{Svc: svc, Loc: loc}, mux)
}

// Serve startet die Weboberfläche auf localhost (kein Auth, daher nur loopback).
func Serve(svc *service.Service, loc *time.Location, port int) error {
	return http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), NewServer(svc, loc))
}
