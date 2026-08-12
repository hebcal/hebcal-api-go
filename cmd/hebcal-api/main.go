// Command hebcal-api is a small HTTP microservice implementing a subset of the
// Hebcal.com REST APIs in Go: the Hebrew Date Converter (JSON, XML, and CSV,
// ported from hebcal-web src/converter.js), Zmanim / Assur Melacha (JSON,
// ported from src/zmanim.js), the Shabbat candle-lighting times, and the
// /geo and /complete location endpoints.
//
// It wires the configuration, logger, and data sources together and starts the
// listener; all request handling lives in internal/handler.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/handler"
	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/leyning"
	"github.com/hebcal/hebcal-api-go/pkg/geodb"
	"github.com/hebcal/hebcal-api-go/pkg/geoip"
)

func main() {
	cfg := config.Load()

	if err := model.LoadNewYork(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot load America/New_York tzdata:", err)
		os.Exit(1)
	}

	lg, err := logger.New(cfg.LogFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open log file:", err)
		os.Exit(1)
	}
	lg.Info("hebcal-api: starting up")

	app := handler.New(lg)
	app.PingFile = cfg.PingFile
	app.GeoIP = geoip.New(cfg.GeoIPSocket)
	app.Leyning = leyning.New(cfg.LeyningURL)

	// Probe the GeoIP unix domain socket at startup so operators see whether
	// the geoip2 service is reachable. A failure is not fatal: /complete still
	// works without IP-based location hints.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	if err := app.GeoIP.DialSocket(probeCtx); err != nil {
		lg.Warn(fmt.Sprintf("GeoIP unix socket %s not reachable: %v", cfg.GeoIPSocket, err))
	} else {
		lg.Info("GeoIP unix socket connected: " + cfg.GeoIPSocket)
	}
	probeCancel()

	// Open the geonames/zips databases for the location-dependent APIs. A
	// failure here is not fatal: those routes report 503 while the other APIs
	// keep working, so an operator can run the server without the location
	// data.
	db, err := geodb.New(cfg.ZipsDB, cfg.GeonamesDB)
	if err != nil {
		lg.Warn("cannot open location databases; /zmanim disabled: " + err.Error())
		fmt.Fprintln(os.Stderr, "warning: /zmanim disabled:", err)
	} else {
		app.DB = db
		defer db.Close()
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// SIGHUP or SIGUSR1 reopens the access log for logrotate
	rotate := make(chan os.Signal, 1)
	signal.Notify(rotate, syscall.SIGHUP, syscall.SIGUSR1)
	go func() {
		for sig := range rotate {
			if err := lg.Reopen(); err != nil {
				fmt.Fprintln(os.Stderr, "log reopen failed:", err)
			} else {
				lg.Info("reopened access log on " + sig.String())
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-stop
		lg.Info("caught " + sig.String() + "; shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	msg := fmt.Sprintf("hebcal-api listening on port %d", cfg.Port)
	lg.Info(msg)
	fmt.Println(msg)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lg.Info("hebcal-api: exiting")
}
