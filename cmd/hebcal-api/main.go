// Command hebcal-api is a small HTTP microservice implementing a subset of the
// Hebcal.com REST APIs in Go: the Hebrew Date Converter (JSON, XML, and CSV,
// ported from hebcal-web src/converter.js), Zmanim / Assur Melacha (JSON,
// ported from src/zmanim.js), the Shabbat candle-lighting times, the /geo and
// /complete location endpoints, and the PDF calendars served from
// download.hebcal.com/v4/ and www.hebcal.com/holidays/ (ported from
// src/pdf.js and src/holidayPdf.js).
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
	"github.com/hebcal/hebcal-api-go/internal/httpx"
	"github.com/hebcal/hebcal-api-go/internal/logger"
	"github.com/hebcal/hebcal-api-go/internal/model"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	mcpsvc "github.com/hebcal/hebcal-api-go/internal/service/mcp"
	"github.com/hebcal/hebcal-api-go/internal/service/pdf"
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
	lg.Info(fmt.Sprintf("hebcal-api %s: starting up", config.APIVersion))

	app := handler.New(lg)
	app.PingFile = cfg.PingFile
	app.GeoIP = geoip.New(cfg.GeoIPSocket)
	app.Readings = readings.New(cfg.ReadingsSocket)

	// The MCP server (www.hebcal.com/mcp). Its tools compute in-process; only
	// torah-portion's reading summary touches readings-svc, and it degrades
	// gracefully without it.
	app.MCP = mcpsvc.Handler(app.Readings)

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

	// Probe readings-svc the same way. A failure is not fatal either, but it is
	// worth saying loudly: without it /shabbat cannot answer with Torah
	// readings and the PDF calendars lose six daily-learning series.
	probeCtx, probeCancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	if err := app.Readings.DialSocket(probeCtx); err != nil {
		lg.Warn(fmt.Sprintf("readings-svc unix socket %s not reachable: %v", cfg.ReadingsSocket, err))
	} else {
		lg.Info("readings-svc unix socket connected: " + cfg.ReadingsSocket)
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
		app.PDF.Geo = db
		defer db.Close()
	}

	// Load the calendar fonts once, at startup: sfnt.Font is read-only after
	// parsing, so every request shares them and only the per-document embedded
	// instances are rebuilt. A failure is not fatal either -- the PDF routes
	// report 503 and the JSON APIs are unaffected.
	if fonts, err := pdf.LoadFonts(cfg.FontDir); err != nil {
		lg.Warn("cannot load fonts; the PDF calendars are disabled: " + err.Error())
		fmt.Fprintln(os.Stderr, "warning: PDF calendars disabled:", err)
	} else {
		app.PDF.Renderer = pdf.NewRenderer(fonts)
	}
	// The six daily-learning series with no Go schedule come from readings-svc.
	// With no socket configured those requests are refused with 501 rather than
	// rendered without the rows the user asked for.
	if cfg.ReadingsSocket != "" {
		app.PDF.Learning = pdf.NewLearningFetcher(app.Readings)
	}

	// Cap concurrent PDF renders so a flood cannot grow the heap into swap (each
	// render churns tens of MiB). The overflow is shed with 503 + Retry-After
	// after a bounded wait. A soft GOMEMLIMIT and a systemd MemoryMax (see
	// etc/hebcal-api.service) are the ceiling behind this cap.
	if cfg.PDFMaxConcurrency > 0 {
		app.PDFLimiter = httpx.NewLimiter(cfg.PDFMaxConcurrency, cfg.PDFQueueTimeout)
		lg.Info(fmt.Sprintf("PDF render concurrency capped at %d (queue timeout %s)",
			cfg.PDFMaxConcurrency, cfg.PDFQueueTimeout))
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

	msg := fmt.Sprintf("hebcal-api %s listening on port %d", config.APIVersion, cfg.Port)
	lg.Info(msg)
	fmt.Println(msg)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lg.Info(fmt.Sprintf("hebcal-api %s: exiting", config.APIVersion))
}
