BIN := hebcal-api
CMD := ./cmd/hebcal-api
# The weekly generator of the static /ical/ calendars, run by gen-static-ical.sh.
STATIC_BIN := hebcal-static-ical
STATIC_CMD := ./cmd/hebcal-static-ical
PREFIX := /usr/local
SVCUSER := www-data
SVCGROUP := hebcal

.PHONY: all build run test vet fmt fmtcheck clean install uninstall sync-webconfig check-webconfig

all: build

# sqlite_fts5 enables the FTS5 extension in mattn/go-sqlite3, required by the
# /complete autocomplete queries against the geoname/ZIP full-text tables.
# sqlite_math_functions enables SQLITE_ENABLE_MATH_FUNCTIONS (e.g. ln()), used
# by the autocomplete relevance-scoring SQL.
GOTAGS := sqlite_fts5,sqlite_math_functions

# This repo ships no geoname/ZIP databases of its own; local dev borrows the
# ones hebcal-web downloads via `node_modules/@hebcal/geo-sqlite/bin/download-and-make-dbs`,
# checked out as a sibling directory. Override GEO_DIR if yours lives
# elsewhere: make run GEO_DIR=/path/to/hebcal-web
GEO_DIR := ../hebcal-web

# hebcal-web's calendar configuration, embedded by internal/webconfig. The
# copies there are never edited by hand: change hebcal-web's, then
#   make sync-webconfig && make test
# and commit the copied files. Override WEB_DIR if hebcal-web lives elsewhere.
WEB_DIR := ../hebcal-web
WEBCONFIG_FILES := dailyLearningConfig.json staticCalendars.json

build:
	CGO_ENABLED=1 go build -tags $(GOTAGS) -trimpath -ldflags="-s -w" -o $(BIN) $(CMD)
	go build -trimpath -ldflags="-s -w" -o $(STATIC_BIN) $(STATIC_CMD)

# Runs the server for local development, listening on :8082 (the port
# hebcal-web's dev servers already point their PDF links at). Rebuilds first,
# so a source change always takes effect: make run
run: build
	./$(BIN) \
		-port 8082 \
		-zips-db $(GEO_DIR)/zips.sqlite3 \
		-geonames-db $(GEO_DIR)/geonames.sqlite3 \
		-fonts fonts

# Copies hebcal-web's calendar configuration into internal/webconfig.
sync-webconfig:
	@for f in $(WEBCONFIG_FILES); do \
		cp $(WEB_DIR)/src/$$f internal/webconfig/$$f || exit 1; \
	done
	@git status --short internal/webconfig

# Fails if the embedded copies differ from hebcal-web's.
check-webconfig:
	@for f in $(WEBCONFIG_FILES); do \
		cmp -s $(WEB_DIR)/src/$$f internal/webconfig/$$f || \
			{ echo "internal/webconfig/$$f differs from $(WEB_DIR)/src/$$f (run make sync-webconfig)"; exit 1; }; \
	done

# Fails, listing the offending files, if anything needs gofmt -- the same
# check CI runs -- so a forgotten `make fmt` breaks `make test` locally too.
test: fmtcheck
	go test -tags $(GOTAGS) ./...

vet:
	go vet -tags $(GOTAGS) ./...

fmt:
	gofmt -w .

fmtcheck:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files need gofmt (run make fmt):"; echo "$$unformatted"; exit 1; \
	fi

clean:
	rm -f $(BIN) $(STATIC_BIN)

# Installs the binary, systemd service, and logrotate config on a
# Debian 13 server. Run as root: make install
install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	install -m 0755 $(STATIC_BIN) $(PREFIX)/bin/$(STATIC_BIN)
	install -d -m 0775 -o $(SVCUSER) -g $(SVCGROUP) /var/log/hebcal
	install -m 0644 etc/$(BIN).service /etc/systemd/system/$(BIN).service
	install -m 0644 etc/$(BIN).logrotate /etc/logrotate.d/$(BIN)
	find fonts -type d -exec install -d -m 0775 /var/www/{} \;
	find fonts -type f -exec install -m 0644 {} /var/www/{} \;
	install -d -m 0755 /var/www/html
	install -m 0644 static/favicon.ico /var/www/html/favicon.ico
	systemctl daemon-reload
	systemctl enable $(BIN).service
	@echo ""
	@echo "Installed. Start the service with:"
	@echo "  systemctl start $(BIN)"

uninstall:
	-systemctl disable --now $(BIN).service
	rm -f /etc/systemd/system/$(BIN).service
	rm -f /etc/logrotate.d/$(BIN)
	rm -f $(PREFIX)/bin/$(BIN) $(PREFIX)/bin/$(STATIC_BIN)
	systemctl daemon-reload
