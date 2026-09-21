package main

import (
	"strings"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/storage"
	"gorbital.dev/modules/storage/s3"
)

// fileStorage opens the file storage STORAGE_DRIVER names (ADR-0075): an
// S3-compatible service for s3, spaces, r2 and minio, set up with
// `orb add storage`. For local, the development default, it returns no store,
// so gorbital keeps its local driver and serves its signed links. gorbital
// has already checked the STORAGE_* variables, and refuses local in
// production.
func fileStorage(cfg gorbital.Config) (storage.Store, error) {
	c := cfg.Storage
	if c.Driver == gorbital.StorageLocal {
		return nil, nil
	}
	local := strings.HasPrefix(c.Endpoint, "127.0.0.1") || strings.HasPrefix(c.Endpoint, "localhost")
	return s3.New(s3.Config{
		Driver: c.Driver, Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket,
		AccessKey: c.AccessKey, SecretKey: c.SecretKey,
		UseSSL: !local, PathStyle: c.PathStyle, PublicURL: c.PublicURL,
	})
}
