package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/pinguladora/imgcull/internal/db"
	"github.com/pinguladora/imgcull/internal/gc"
	"github.com/pinguladora/imgcull/internal/runtime"
)

const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB
	TiB = 1024 * GiB

	defaultDbPath            = "imgcull.db"
	defaultDeletionChunkSize = 10
	defaultDeletionSleepMs   = 200
	defaultKeepLabel         = "imgcull-keep"
	defaultDryRun            = false
	defaultMaxUnused         = "20G"
	defaultMinAgeHours       = 24
	defaultRuntime           = "docker"
	defaultPollIntervalSec   = 60
)

func initLogger() {
	zerolog.TimeFieldFormat = time.RFC3339
	if os.Getenv("PLAIN_LOG") == "1" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		log.Logger = log.Output(os.Stderr)
	}
	log.Logger = log.With().Str("component", "imgcull").Logger()
}

func parseHumanSize(size string) (int64, error) {
	if size == "" {
		return 0, errors.New("empty size")
	}
	t := strings.TrimSpace(strings.ToUpper(size))

	multiplier, suffixLen := parseSizeMultiplier(t)
	if suffixLen > 0 {
		t = strings.TrimSpace(t[:len(t)-suffixLen])
	}

	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		if multiplier == 1 {
			if i, err2 := strconv.ParseInt(t, 10, 64); err2 == nil {
				return i, nil
			}
		}
		return 0, fmt.Errorf("invalid size %q: %w", size, err)
	}

	bytes := int64(v * float64(multiplier))
	if bytes < 0 {
		return 0, fmt.Errorf("size overflow for %q", size)
	}
	return bytes, nil
}

func parseSizeMultiplier(t string) (int64, int) {
	if len(t) == 0 {
		return 1, 0
	}
	last := t[len(t)-1]
	switch last {
	case 'K':
		return KiB, 1
	case 'M':
		return MiB, 1
	case 'G':
		return GiB, 1
	case 'T':
		return TiB, 1
	default:
		return 1, 0
	}
}

func main() {
	initLogger()

	if err := run(); err != nil {
		log.Error().Err(err).Msg("fatal error")
		os.Exit(1)
	}
}

func run() error {
	runtimeFlag := flag.String("runtime", defaultRuntime, "runtime to use: podman|docker|nerdctl")
	maxUnused := flag.String("max-unused-bytes", defaultMaxUnused, "max allowed unused image bytes before GC (eg. 20G)")
	poll := flag.Int("poll-interval", defaultPollIntervalSec, "seconds between reconciliation runs")
	dbPath := flag.String("db-path", defaultRuntime+"_"+defaultDbPath, "path to bolt DB (eg. docker_imgcull.db)")
	keepLabel := flag.String("keep-label", defaultKeepLabel, "label name that prevents deletion")
	dry := flag.Bool("dry-run", defaultDryRun, "don't actually delete images")
	minAge := flag.Int("min-age-hours", defaultMinAgeHours, "min image age before deletion (hours)")
	chunkSize := flag.Int("deletion-chunk-size", defaultDeletionChunkSize, "max images to delete per reconcile run")
	sleepMs := flag.Int("deletion-sleep-ms", defaultDeletionSleepMs, "ms to sleep between deletions")
	flag.Parse()

	mu, err := parseHumanSize(*maxUnused)
	if err != nil {
		return fmt.Errorf("invalid max-unused-bytes: %w", err)
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Error().Err(err).Msg("close db")
		}
	}()

	adapter, ok := getAdapter(*runtimeFlag)
	if !ok {
		return fmt.Errorf("unsupported runtime: %s", *runtimeFlag)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctrl := gc.NewController(ctx, adapter, database, mu, *poll, *keepLabel, *dry, *minAge, *chunkSize, *sleepMs)

	if err := ctrl.Seed(ctx); err != nil {
		log.Error().Err(err).Msg("seed failed")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	ctrl.RunLoop(ctx)
	return nil
}

func getAdapter(name string) (runtime.Adapter, bool) {
	switch name {
	case "podman":
		return runtime.NewPodmanAdapter(), true
	case "docker":
		return runtime.NewDockerAdapter(), true
	case "nerdctl":
		return runtime.NewNerdctlAdapter(), true
	default:
		return nil, false
	}
}
