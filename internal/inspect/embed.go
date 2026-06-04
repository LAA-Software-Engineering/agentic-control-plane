package inspect

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webEmbed embed.FS

func (s *Server) staticFS() http.Handler {
	sub, err := fs.Sub(webEmbed, "web/static")
	if err != nil {
		return http.FileServer(http.FS(webEmbed))
	}
	return http.FileServer(http.FS(sub))
}

func (s *Server) staticHandler(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := webEmbed.ReadFile("web/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if name == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}
