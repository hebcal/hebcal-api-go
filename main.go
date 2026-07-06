// hebcal-converter is a small HTTP microservice implementing the Hebcal
// Hebrew Date Converter REST APIs (JSON, XML, and CSV), ported from the
// Node.js implementation in hebcal-web src/converter.js.
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

func main() {
	defaultPort := 8082
	if s := os.Getenv("PORT"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			defaultPort = n
		}
	}
	port := flag.Int("port", defaultPort, "port to listen on")
	logFile := flag.String("logfile", "/var/log/hebcal/converter.log",
		"access log file path (\"-\" for stdout)")
	pingFile := flag.String("pingfile", defaultPingFile,
		"file served by /ping; a missing file makes /ping return 404")
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
	logger.Info("hebcal-converter: starting up")

	app := newAppServer(logger)
	app.pingFile = *pingFile
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

	msg := fmt.Sprintf("hebcal-converter listening on port %d", *port)
	logger.Info(msg)
	fmt.Println(msg)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logger.Info("hebcal-converter: exiting")
}
