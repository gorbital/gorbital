package app

import (
	"os"

	"gorbital.dev/modules/storage/logarchive"
)

// The hourly log archive (ADR-0079): with the logs.archive.enabled runtime
// setting on, every log record is also written as a JSON line to a file
// for the current hour under LOG_ARCHIVE_DIR, and each finished hour (and
// the partial hour at shutdown) is gzipped and stored in the app's file
// storage as logs/<service>/<YYYY>/<MM>/<DD>/<HH>.<host>.jsonl.gz, where
// /ops/storage and the Dev Portal's Storage screen list it. Off (the
// default), nothing is collected.

// newLogArchive builds the archive for this instance. Its handler joins
// the logger in newBase; build binds it to the store and the setting.
func newLogArchive(cfg Config) (*logarchive.Archive, error) {
	host, _ := os.Hostname() // empty on failure: keys then carry no instance
	return logarchive.New(cfg.LogArchiveDir, ServiceName,
		logarchive.WithLevel(cfg.LogLevel), // the archive holds what the console shows
		logarchive.WithInstance(host),      // instances sharing a bucket keep their own hours
	)
}
