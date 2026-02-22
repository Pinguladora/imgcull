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

func initLogger() {
	zerolog.TimeFieldFormat = time.RFC3339
	if os.Getenv("PLAIN_LOG") == "1" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		log.Logger = log.Output(os.Stderr)
	}
	log.Logger = log.With().Str("component", "imgcull").Logger()
}

const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB
	TiB = 1024 * GiB
)

// parseHumanSize parses sizes like "20G", "512M", "1.5G", "1024" and returns bytes.
func parseHumanSize(size string) (int64, error) {
	if size == "" {
		return 0, errors.New("empty size")
	}
	t := strings.TrimSpace(strings.ToUpper(size))

	// handle pure integer (no suffix)
	// but also allow floats with suffix (e.g. 1.5G)
	var multiplier int64 = 1
	last := t[len(t)-1]

	switch last {
	case 'K':
		multiplier = KiB
		t = strings.TrimSpace(t[:len(t)-1])
	case 'M':
		multiplier = MiB
		t = strings.TrimSpace(t[:len(t)-1])
	case 'G':
		multiplier = GiB
		t = strings.TrimSpace(t[:len(t)-1])
	case 'T':
		multiplier = TiB
		t = strings.TrimSpace(t[:len(t)-1])
	default:
		// keep multiplier = 1 and t unchanged
	}

	// parse numeric part (allow integers and floats)
	v, err := strconv.ParseFloat(t, 64)
	if err != nil {
		// if the string contained no suffix and is an integer-like string, try ParseInt
		if multiplier == 1 {
			if i, err2 := strconv.ParseInt(t, 10, 64); err2 == nil {
				return i, nil
			}
		}
		return 0, fmt.Errorf("invalid size %q: %w", size, err)
	}

	// compute bytes and guard overflow
	bytes := int64(v * float64(multiplier))
	if bytes < 0 {
		return 0, fmt.Errorf("size overflow for %q", size)
	}
	return bytes, nil
}

func main() {
	initLogger()

	runtimeFlag := flag.String("runtime", "podman", "runtime to use: podman|docker|nerdctl")
	maxUnused := flag.String("max-unused-bytes", "20G", "max allowed unused image bytes before GC (eg 20G)")
	poll := flag.Int("poll-interval", 60, "seconds between reconciliation runs")
	dbPath := flag.String("db-path", "imgcull.db", "path to bolt DB")
	keepLabel := flag.String("keep-label", "keep", "label name that prevents deletion")
	dry := flag.Bool("dry-run", false, "don't actually delete images")
	minAge := flag.Int("min-age-hours", 1, "min image age before deletion (hours)")
	chunkSize := flag.Int("deletion-chunk-size", 10, "max images to delete per reconcile run")
	sleepMs := flag.Int("deletion-sleep-ms", 200, "ms to sleep between deletions")
	flag.Parse()

	mu, err := parseHumanSize(*maxUnused)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid max-unused-bytes")
	}

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer database.Close()

	var adapter runtime.Adapter
	switch *runtimeFlag {
	case "podman":
		adapter = runtime.NewPodmanAdapter()
	case "docker":
		adapter = runtime.NewDockerAdapter()
	case "nerdctl":
		adapter = runtime.NewNerdctlAdapter()
	default:
		log.Fatal().Msgf("unsupported runtime: %s", *runtimeFlag)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctrl := gc.NewController(ctx, adapter, database, mu, *poll, *keepLabel, *dry, *minAge, *chunkSize, *sleepMs)

	if err := ctrl.Seed(); err != nil {
		log.Error().Err(err).Msg("seed failed")
	}

	// graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	ctrl.RunLoop()
}
