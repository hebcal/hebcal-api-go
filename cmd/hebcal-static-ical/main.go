// Command hebcal-static-ical writes the static calendars served from
// download.hebcal.com/ical/ -- the holiday, Hebrew-date, Omer, Torah-reading,
// Yizkor and Shabbat Mevarchim feeds, one feed per daily-learning series, and
// Kindness a Day -- as an .ics and a .csv each, with brotli and gzip copies
// next to them for the server to send precompressed.
//
// It runs weekly from gen-static-ical.sh, and needs readings-svc for the Torah
// readings and the learning series hebcal-go has no schedule for.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata"

	"github.com/andybalholm/brotli"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
	"github.com/hebcal/hebcal-api-go/internal/service/staticcal"
)

func main() {
	dir := flag.String("dir", "ical", "directory to write the files into")
	socket := flag.String("readings-socket", readings.DefaultSocket, "readings-svc unix domain socket")
	noCompress := flag.Bool("nocompress", false, "skip writing the .br and .gz copies")
	quiet := flag.Bool("quiet", false, "only report errors")
	verbose := flag.Bool("verbose", false, "report each file written")
	todayFlag := flag.String("today", "", "generate as of this date (YYYY-MM-DD) rather than now")
	only := flag.String("only", "", "generate just this feed (its file name without extension)")
	flag.Parse()
	log.SetFlags(0)

	today := time.Now()
	if *todayFlag != "" {
		t, err := time.ParseInLocation(time.DateOnly, *todayFlag, time.Local)
		if err != nil {
			log.Fatalf("-today: %v", err)
		}
		today = t.Add(12 * time.Hour)
	}
	// Every file is stamped with the minute generation began, as its DTSTAMP
	// and as its modification time, which the server reports as
	// Last-Modified.
	today = today.Truncate(time.Minute)

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	w := &writer{dir: *dir, mtime: today, compress: !*noCompress, verbose: *verbose}
	rs := readings.New(*socket)
	ctx := context.Background()
	failed := 0
	for i := range staticcal.Feeds {
		f := &staticcal.Feeds[i]
		if *only != "" && f.File != *only {
			continue
		}
		files, err := staticcal.Render(ctx, f, today, rs)
		if err != nil {
			// One feed failing -- a learning cycle that has ended, a
			// readings-svc too old to know a series -- leaves that feed's
			// last good files in place rather than stopping the rest.
			log.Print(err)
			failed++
			continue
		}
		if !*quiet {
			log.Printf("%s: %d", f.File, files.Events)
		}
		w.write(f.File, files)
	}
	if *only == "" || *only == staticcal.KindnessFile {
		files, err := staticcal.Kindness(today)
		if err != nil {
			log.Fatal(err)
		}
		if !*quiet {
			log.Printf("%s: %d", staticcal.KindnessFile, files.Events)
		}
		w.write(staticcal.KindnessFile, files)
	}
	if failed > 0 {
		log.Fatalf("%d feeds failed", failed)
	}
	if !*quiet {
		log.Print("Done")
	}
}

// writer puts each feed's files in place.
type writer struct {
	dir      string
	mtime    time.Time
	compress bool
	verbose  bool
}

func (w *writer) write(name string, files *staticcal.Files) {
	for _, f := range []struct {
		ext  string
		data []byte
	}{{".ics", files.ICS}, {".csv", files.CSV}} {
		path := filepath.Join(w.dir, name+f.ext)
		w.put(path, f.data)
		if w.compress {
			w.put(path+".br", compressBrotli(f.data))
			w.put(path+".gz", compressGzip(f.data, name+f.ext, w.mtime))
		} else {
			// A precompressed copy left from an earlier run would be served
			// in place of the new file.
			for _, ext := range []string{".br", ".gz"} {
				if err := os.Remove(path + ext); err != nil && !os.IsNotExist(err) {
					log.Fatal(err)
				}
			}
		}
	}
}

// put writes data to path through a temporary file renamed into place, so
// the server never sends a half-written file, and stamps it with the
// generation time.
func (w *writer) put(path string, data []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		log.Fatal(err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Chtimes(tmp.Name(), w.mtime, w.mtime)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		log.Fatal(err)
	}
	if w.verbose {
		log.Printf("wrote %s (%d bytes)", path, len(data))
	}
}

// compressBrotli compresses at the highest quality, as `brotli --best` does.
func compressBrotli(data []byte) []byte {
	var buf bytes.Buffer
	zw := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: brotli.BestCompression, LGWin: 24})
	mustCopy(zw, data)
	return buf.Bytes()
}

// compressGzip compresses at the highest level, as `gzip --best` does,
// recording the file's name and modification time in the header.
func compressGzip(data []byte, name string, mtime time.Time) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Name = name
	zw.ModTime = mtime
	mustCopy(zw, data)
	return buf.Bytes()
}

func mustCopy(zw io.WriteCloser, data []byte) {
	if _, err := zw.Write(data); err != nil {
		log.Fatal(fmt.Errorf("compressing: %w", err))
	}
	if err := zw.Close(); err != nil {
		log.Fatal(fmt.Errorf("compressing: %w", err))
	}
}
