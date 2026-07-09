BIN := hebcal-api
PREFIX := /usr/local
SVCUSER := www-data

.PHONY: all build test vet fmt clean install uninstall

all: build

# sqlite_fts5 enables the FTS5 extension in mattn/go-sqlite3, required by the
# /complete autocomplete queries against the geoname/ZIP full-text tables.
GOTAGS := sqlite_fts5

build:
	CGO_ENABLED=1 go build -tags $(GOTAGS) -trimpath -ldflags="-s -w" -o $(BIN) .

test:
	go test -tags $(GOTAGS) ./...

vet:
	go vet -tags $(GOTAGS) ./...

fmt:
	gofmt -w *.go

clean:
	rm -f $(BIN)

# Installs the binary, systemd service, and logrotate config on a
# Debian 13 server. Run as root: make install
install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	install -d -o $(SVCUSER) -g $(SVCUSER) /var/log/hebcal
	install -m 0644 etc/$(BIN).service /etc/systemd/system/$(BIN).service
	install -m 0644 etc/$(BIN).logrotate /etc/logrotate.d/$(BIN)
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
