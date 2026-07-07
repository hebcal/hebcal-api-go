// hebcal-api is a small HTTP microservice implementing a subset of the
// Hebcal.com REST APIs in Go: the Hebrew Date Converter (JSON, XML, and CSV,
// ported from hebcal-web src/converter.js) and Zmanim / Assur Melacha (JSON,
// ported from src/zmanim.js).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata"
)

// envOr returns the environment variable named by key, or def if it is unset
// or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	defaultPort := 8082
	if s := os.Getenv("PORT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			defaultPort = n
		}
	}
	port := flag.Int("port", defaultPort, "port to listen on")
	logFile := flag.String("logfile", "", "access log file path (empty or \"-\" for stdout)")
	pingFile := flag.String("pingfile", defaultPingFile,
		"file served by /ping; a missing file makes /ping return 404")
	zipsDB := flag.String("zips-db", envOr("ZIPS_DB", "zips.sqlite3"),
		"path to the zips SQLite database (for the /zmanim API)")
	geonamesDB := flag.String("geonames-db", envOr("GEONAMES_DB", "geonames.sqlite3"),
		"path to the geonames SQLite database (for the /zmanim API)")
	flag.Parse()

	var err error
	nyLoc, err = time.LoadLocation("America/New_York")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot load America/New_York tzdata:", err)
		os.Exit(1)
	}

	logger, err := newAccessLogger(*logFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open log file:", err)
		os.Exit(1)
	}
	logger.Info("hebcal-api: starting up")

	app := newAppServer(logger)
	app.pingFile = *pingFile

	// Open the geonames/zips databases for the /zmanim API. A failure here is
	// not fatal: the /zmanim route reports 503 while the other APIs keep
	// working, so an operator can run the server without the location data.
	db, err := NewGeoDB(*zipsDB, *geonamesDB)
	if err != nil {
		logger.Info("cannot open location databases; /zmanim disabled: " + err.Error())
		fmt.Fprintln(os.Stderr, "warning: /zmanim disabled:", err)
	} else {
		app.db = db
		defer db.Close()
	}
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           app.mux(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// SIGHUP or SIGUSR1 reopens the access log for logrotate
	rotate := make(chan os.Signal, 1)
	signal.Notify(rotate, syscall.SIGHUP, syscall.SIGUSR1)
	go func() {
		for sig := range rotate {
			if err := logger.Reopen(); err != nil {
				fmt.Fprintln(os.Stderr, "log reopen failed:", err)
			} else {
				logger.Info("reopened access log on " + sig.String())
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-stop
		logger.Info("caught " + sig.String() + "; shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	msg := fmt.Sprintf("hebcal-api listening on port %d", *port)
	logger.Info(msg)
	fmt.Println(msg)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger.Info("hebcal-api: exiting")
}
