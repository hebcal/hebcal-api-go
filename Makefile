BIN := hebcal-api
CMD := ./cmd/hebcal-api
PREFIX := /usr/local
SVCUSER := www-data
SVCGROUP := hebcal

.PHONY: all build run test vet fmt clean install uninstall

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

build:
	CGO_ENABLED=1 go build -tags $(GOTAGS) -trimpath -ldflags="-s -w" -o $(BIN) $(CMD)

# Runs the server for local development, listening on :8082 (the port
# hebcal-web's dev servers already point their PDF links at). Rebuilds first,
# so a source change always takes effect: make run
run: build
	./$(BIN) \
		-port 8082 \
		-zips-db $(GEO_DIR)/zips.sqlite3 \
		-geonames-db $(GEO_DIR)/geonames.sqlite3 \
		-fonts fonts

test:
	go test -tags $(GOTAGS) ./...

vet:
	go vet -tags $(GOTAGS) ./...

fmt:
	gofmt -w cmd internal pkg

clean:
	rm -f $(BIN)

# Installs the binary, systemd service, and logrotate config on a
# Debian 13 server. Run as root: make install
install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	install -d -m 0775 -o $(SVCUSER) -g $(SVCGROUP) /var/log/hebcal
	install -m 0644 etc/$(BIN).service /etc/systemd/system/$(BIN).service
	install -m 0644 etc/$(BIN).logrotate /etc/logrotate.d/$(BIN)
	find fonts -type d -exec install -d -m 0775 /var/www/{} \;
	find fonts -type f -exec install -m 0644 {} /var/www/{} \;
	systemctl daemon-reload
	systemctl enable $(BIN).service
	@echo ""
	@echo "Installed. Start the service with:"
	@echo "  systemctl start $(BIN)"

uninstall:
	-systemctl disable --now $(BIN).service
	rm -f /etc/systemd/system/$(BIN).service
	rm -f /etc/logrotate.d/$(BIN)
	rm -f $(PREFIX)/bin/$(BIN)
	systemctl daemon-reload
