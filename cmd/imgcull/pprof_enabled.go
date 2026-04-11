//go:build pprof

package main

import (
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/rs/zerolog/log"
)

func init() {
	addr := os.Getenv("PPROF_ADDR")
	if addr == "" {
		addr = "localhost:6060"
	}
	go func() {
		log.Info().Str("addr", addr).Msg("pprof server started")
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Error().Err(err).Msg("pprof server failed")
		}
	}()
}
