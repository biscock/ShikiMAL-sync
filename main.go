package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shikimal-sync/internal/config"
	"shikimal-sync/internal/mal"
	"shikimal-sync/internal/shikimori"
	"shikimal-sync/internal/storage"
	"shikimal-sync/internal/syncer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return errors.New("command is required")
	}

	command := os.Args[1]
	configPath := "config.json"
	if len(os.Args) >= 3 {
		configPath = os.Args[2]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shikiStore := storage.NewTokenStore(cfg.ShikimoriTokenPath())
	malStore := storage.NewTokenStore(cfg.MALTokenPath())
	shikiClient := shikimori.NewClient(cfg.Shikimori, cfg.AppName, shikiStore)
	malClient := mal.NewClient(cfg.MyAnimeList, malStore)

	switch command {
	case "auth-shiki":
		return shikiClient.Authorize(ctx)
	case "auth-mal":
		return malClient.Authorize(ctx)
	case "once":
		engine := syncer.NewEngine(shikiClient, malClient, storage.NewStateStore(cfg.StatePath()))
		return runOnce(ctx, engine)
	case "run":
		engine := syncer.NewEngine(shikiClient, malClient, storage.NewStateStore(cfg.StatePath()))
		return runLoop(ctx, cfg, engine)
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func runOnce(ctx context.Context, engine *syncer.Engine) error {
	result, err := engine.RunOnce(ctx)
	if err != nil {
		return err
	}
	logCycleResult("sync cycle", result)
	return nil
}

func runLoop(ctx context.Context, cfg *config.Config, engine *syncer.Engine) error {
	interval, err := cfg.PollDuration()
	if err != nil {
		return err
	}

	fmt.Printf("Starting sync loop. Poll interval: %s\n", interval)
	result, err := engine.RunOnce(ctx)
	if err != nil {
		return err
	}
	logCycleResult("startup cycle", result)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Stopping sync loop.")
			return nil
		case <-ticker.C:
			result, err := engine.RunOnce(ctx)
			if err != nil {
				fmt.Printf("Sync cycle failed: %v\n", err)
				continue
			}
			logCycleResult("sync cycle", result)
		}
	}
}

func logCycleResult(prefix string, result syncer.CycleResult) {
	fmt.Printf("%s finished: baseline=%d updated=%d deleted=%d\n", prefix, result.Baselined, result.Updated, result.Deleted)
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  shikimal-sync auth-shiki [config.json]")
	fmt.Println("  shikimal-sync auth-mal [config.json]")
	fmt.Println("  shikimal-sync once [config.json]")
	fmt.Println("  shikimal-sync run [config.json]")
}
