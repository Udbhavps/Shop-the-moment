package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	cat, err := LoadCatalog()
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}
	web, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	if dir := os.Getenv("WEB_DIR"); dir != "" { // live-edit the UI without rebuilding
		log.Printf("serving web assets from %s", dir)
		web = os.DirFS(dir)
	}
	srv := NewServer(cat, NewStore(), NewGeminiFromEnv(), web)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	hs := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		log.Printf("Shop the Moment listening on :%s (%d products, live AI: %v)", port, len(cat.All()), srv.gem != nil)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hs.Shutdown(ctx)
}
