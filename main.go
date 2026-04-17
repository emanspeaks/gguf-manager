package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

func main() {
	configPath := flag.String("config", "", "path to JSONC config file")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	dl := newDownloader(cfg)
	srv := newServer(cfg, dl)

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /api/local", srv.handleLocal)
	mux.HandleFunc("GET /api/repo", srv.handleRepo)
	mux.HandleFunc("POST /api/download", srv.handleDownload)
	mux.HandleFunc("GET /api/download/status", srv.handleDownloadStatus)
	mux.HandleFunc("DELETE /api/local/{name}", srv.handleDeleteLocal)
	mux.HandleFunc("GET /api/status", srv.handleStatus)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("gguf-manager listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
