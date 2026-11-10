// Package yahrzeitdb reads saved Yahrzeit + Anniversary calendars from the
// site's MySQL database, where each is a row keyed by its ULID.
//
// The repository does two things: a primary-key read of the calendar, and
// two bookkeeping writes after it -- marking the calendar downloaded (the
// email retention job prunes calendars that never were) and stamping its last
// access. The writes are fire-and-forget on a single background worker, so a
// slow database costs the feed nothing and cannot pile up goroutines.
package yahrzeitdb

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Timeouts for the read the response waits on and for the bookkeeping writes
// it does not.
const (
	readTimeout  = 7 * time.Second
	writeTimeout = 3 * time.Second
)

// Pool sizing. The managed database drops idle connections, so a pooled
// connection is retired well before it would be.
const (
	maxOpenConns    = 8
	maxIdleConns    = 4
	connMaxLifetime = 5 * time.Minute
	connMaxIdleTime = 1 * time.Minute
)

// touchQueue is how many bookkeeping writes may wait for the worker. A full
// queue drops the write rather than block a response; the next poll of the
// same calendar writes it again.
const touchQueue = 256

// ErrNotFound is returned by Get for an id with no saved calendar.
var ErrNotFound = errors.New("yahrzeit calendar not found")

// Row is a saved calendar.
type Row struct {
	// Contents is the calendar's JSON object, as the database returns it.
	Contents []byte
	// Updated is when the calendar was last saved.
	Updated time.Time
	// Downloaded reports whether the calendar had been fetched before.
	Downloaded bool
}

// Store is what the download handler needs from the database; tests supply
// an in-memory one.
type Store interface {
	// Get reads the calendar saved under id, or returns ErrNotFound.
	Get(ctx context.Context, id string) (*Row, error)
	// Touch records that the calendar was fetched. It does not wait for the
	// database, and failures are only logged.
	Touch(id string, downloaded bool)
}

// Config is how to reach the database.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	// TLS is the driver's tls setting: "preferred" (the default: TLS when the
	// server offers it, unverified, since the connection stays on the private
	// network), "true", "skip-verify" or "false".
	TLS string
}

// ConfigFromINI reads the hebcal.mysql.* keys of hebcal-dot-com.ini. ok is
// false when the file does not name a user and database, in which case the
// saved calendars are simply not served.
func ConfigFromINI(ini map[string]string) (cfg Config, ok bool) {
	cfg = Config{
		Host:     ini["hebcal.mysql.host"],
		User:     ini["hebcal.mysql.user"],
		Password: ini["hebcal.mysql.password"],
		DBName:   ini["hebcal.mysql.dbname"],
		TLS:      ini["hebcal.mysql.tls"],
		Port:     3306,
	}
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if p, err := strconv.Atoi(ini["hebcal.mysql.port"]); err == nil && p > 0 {
		cfg.Port = p
	}
	if cfg.TLS == "" {
		cfg.TLS = "preferred"
	}
	return cfg, cfg.User != "" && cfg.DBName != ""
}

// String describes the connection without its password, for the log.
func (c Config) String() string {
	return "mysql://" + c.User + "@" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + "/" + c.DBName
}

// DB is the MySQL-backed Store.
type DB struct {
	db    *sql.DB
	touch chan touchJob
	warn  func(string)
	done  chan struct{}
}

type touchJob struct {
	id         string
	downloaded bool
}

// Open returns a DB for cfg. It does not connect: the pool dials on first
// use, so the service starts whether or not the database is reachable yet.
// warn receives the failures of the background writes.
func Open(cfg Config, warn func(string)) (*DB, error) {
	mc := mysql.NewConfig()
	mc.User = cfg.User
	mc.Passwd = cfg.Password
	mc.Net = "tcp"
	mc.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	mc.DBName = cfg.DBName
	mc.TLSConfig = cfg.TLS
	mc.ParseTime = true
	mc.Timeout = 5 * time.Second
	connector, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)
	db.SetConnMaxIdleTime(connMaxIdleTime)
	if warn == nil {
		warn = func(string) {}
	}
	d := &DB{db: db, touch: make(chan touchJob, touchQueue), warn: warn, done: make(chan struct{})}
	go d.worker()
	return d, nil
}

// Ping checks that the database answers.
func (d *DB) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }

// Close stops the worker, after the writes already queued, and closes the
// pool.
func (d *DB) Close() error {
	close(d.touch)
	<-d.done
	return d.db.Close()
}

// Get implements Store.
func (d *DB) Get(ctx context.Context, id string) (*Row, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var row Row
	err := d.db.QueryRowContext(ctx,
		"SELECT contents, updated, downloaded FROM yahrzeit WHERE id = ?", id).
		Scan(&row.Contents, &row.Updated, &row.Downloaded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Touch implements Store.
func (d *DB) Touch(id string, downloaded bool) {
	select {
	case d.touch <- touchJob{id, downloaded}:
	default:
		d.warn("yahrzeitdb: write queue full, dropped access time for " + id)
	}
}

func (d *DB) worker() {
	defer close(d.done)
	for job := range d.touch {
		if !job.downloaded {
			d.exec("UPDATE yahrzeit SET downloaded = 1, updated = NOW() WHERE id = ?", job.id)
		}
		d.exec("REPLACE INTO yahrzeit_atime (id, ts) VALUES (?, NOW())", job.id)
	}
}

func (d *DB) exec(query, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if _, err := d.db.ExecContext(ctx, query, id); err != nil {
		d.warn("yahrzeitdb: " + err.Error())
	}
}
