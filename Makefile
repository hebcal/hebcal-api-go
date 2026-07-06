BIN := hebcal-converter
PREFIX := /usr/local
SVCUSER := www-data

.PHONY: all build test vet fmt clean install uninstall

all: build

build:
	go build -trimpath -ldflags="-s -w" -o $(BIN) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w *.go

clean:
	rm -f $(BIN)

# Installs the binary, systemd service, and logrotate config on a
# Debian 13 server. Run as root: make install
install: build
	install -m 0755 $(BIN) $(PREFIX)/bin/$(BIN)
	install -d -o $(SVCUSER) -g $(SVCUSER) /var/log/hebcal
	install -m 0644 etc/hebcal-converter.service /etc/systemd/system/hebcal-converter.service
	install -m 0644 etc/hebcal-converter.logrotate /etc/logrotate.d/hebcal-converter
	systemctl daemon-reload
	systemctl enable hebcal-converter.service
	@echo ""
	@echo "Installed. Start the service with:"
	@echo "  systemctl start hebcal-converter"

uninstall:
	-systemctl disable --now hebcal-converter.service
	rm -f /etc/systemd/system/hebcal-converter.service
	rm -f /etc/logrotate.d/hebcal-converter
	rm -f $(PREFIX)/bin/$(BIN)
	systemctl daemon-reload
