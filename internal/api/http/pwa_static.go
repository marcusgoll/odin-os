package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed app_static
var pwaStatic embed.FS

func registerPWAHandlers(mux *http.ServeMux) {
	subtree, err := fs.Sub(pwaStatic, "app_static")
	if err != nil {
		panic(err)
	}

	mux.HandleFunc("GET /app", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/app/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /app/", func(writer http.ResponseWriter, request *http.Request) {
		assetPath := strings.TrimPrefix(request.URL.Path, "/app/")
		if assetPath == "" {
			assetPath = "index.html"
		}
		assetPath = path.Clean(assetPath)
		if strings.HasPrefix(assetPath, "..") {
			http.NotFound(writer, request)
			return
		}
		setPWAContentType(writer, assetPath)
		http.ServeFileFS(writer, request, subtree, assetPath)
	})
}

func setPWAContentType(writer http.ResponseWriter, assetPath string) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	switch path.Ext(assetPath) {
	case ".webmanifest":
		writer.Header().Set("Content-Type", "application/manifest+json")
	case ".js":
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".svg":
		writer.Header().Set("Content-Type", "image/svg+xml")
	case ".html":
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
}
