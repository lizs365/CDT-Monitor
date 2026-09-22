package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/wang4386/CDT-Monitor/internal/aliyun"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/engine"
	"github.com/wang4386/CDT-Monitor/internal/httpapi"
	"github.com/wang4386/CDT-Monitor/internal/notify"
	"github.com/wang4386/CDT-Monitor/internal/store"
	"github.com/wang4386/CDT-Monitor/internal/web"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	command := "serve"
	if len(os.Args) > 1 && os.Args[1][0] != '-' {
		command = os.Args[1]
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	dataDir := flag.String("data", envOr("CDT_DATA_DIR", "./data"), "persistent data directory")
	listen := flag.String("listen", envOr("CDT_LISTEN", ":8080"), "HTTP listen address")
	workers := flag.Int("workers", envInt("CDT_WORKERS", 4), "background worker count")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if command == "version" {
		fmt.Printf("cdt-monitor %s (%s, %s, %s/%s)\n", version, commit, builtAt, runtime.GOOS, runtime.GOARCH)
		return
	}
	if command == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, checkErr := client.Get("http://127.0.0.1" + normalizeListen(*listen) + "/healthz")
		if checkErr != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				response.Body.Close()
			}
			os.Exit(1)
		}
		response.Body.Close()
		return
	}
	st, err := store.Open(*dataDir)
	if err != nil {
		logger.Error("open data store", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	if command == "migrate" {
		logger.Info("database migrations complete", "data_dir", *dataDir)
		return
	}

	provider := aliyun.NewClient()
	notifier := notify.New()
	extraRegions := loadExtraRegions(*dataDir, logger)
	eng := engine.New(st, provider, notifier, logger, *workers, extraRegions...)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	eng.Start(ctx)

	if command == "run-once" {
		if err = eng.RunOnce(ctx); err != nil {
			logger.Error("run monitor cycle", "error", err)
			os.Exit(1)
		}
		deadline := time.Now().Add(70 * time.Second)
		for time.Now().Before(deadline) {
			count, countErr := st.CountQueuedJobs(ctx)
			if countErr != nil || count == 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		logger.Info("monitor cycle complete")
		return
	}
	if command != "serve" {
		logger.Error("unknown command", "command", command)
		os.Exit(2)
	}

	api := httpapi.New(st, eng, web.FS(), logger, extraRegions, httpapi.BuildInfo{Version: version, Commit: commit, BuiltAt: builtAt})
	server := &http.Server{
		Addr: *listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
	}
	go func() {
		logger.Info("CDT Monitor started", "listen", *listen, "version", version, "data_dir", *dataDir)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", serveErr)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("CDT Monitor stopped")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// loadExtraRegions reads regions.json from the data directory. The file is
// optional: a missing file is ignored, while a malformed file is reported and
// skipped so the built-in region list still works.
func loadExtraRegions(dataDir string, logger *slog.Logger) []domain.Region {
	path := filepath.Join(dataDir, "regions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Error("read regions config", "path", path, "error", err)
		}
		return nil
	}
	var parsed []domain.Region
	if err = json.Unmarshal(data, &parsed); err != nil {
		logger.Error("parse regions config", "path", path, "error", err)
		return nil
	}
	seen := make(map[string]bool, len(parsed))
	regions := make([]domain.Region, 0, len(parsed))
	for _, region := range parsed {
		region.Value = strings.TrimSpace(region.Value)
		region.Label = strings.TrimSpace(region.Label)
		if region.Value == "" || region.Label == "" || seen[region.Value] || len(regions) >= 100 {
			continue
		}
		seen[region.Value] = true
		regions = append(regions, region)
	}
	if len(regions) > 0 {
		logger.Info("loaded extra regions", "path", path, "count", len(regions))
	}
	return regions
}
func envInt(key string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil && value > 0 {
		return value
	}
	return fallback
}

func normalizeListen(listen string) string {
	if len(listen) > 0 && listen[0] == ':' {
		return listen
	}
	if _, port, err := net.SplitHostPort(listen); err == nil {
		return ":" + port
	}
	return ":8080"
}
