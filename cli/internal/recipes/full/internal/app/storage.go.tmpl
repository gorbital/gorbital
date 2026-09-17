package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/modules/storage"
	"gorbital.dev/modules/storage/local"
	"gorbital.dev/modules/storage/s3"
)

// File storage (ADR-0075): a bucket of objects the app reads and writes
// through gorbital.dev/modules/storage. The driver comes from
// STORAGE_DRIVER: local (the development default, files under
// STORAGE_LOCAL_DIR) or an S3-compatible service (s3, spaces, r2, minio,
// with STORAGE_ENDPOINT, STORAGE_REGION, STORAGE_BUCKET and the keys).
// Production refuses local. Operators browse it at /ops/storage, and the
// Dev Portal's Storage screen builds on that.
const (
	storageDriverLocal  = "local"
	storageDriverS3     = "s3"
	storageDriverSpaces = "spaces"
	storageDriverR2     = "r2"
	storageDriverMinIO  = "minio"
	// storageLocalPath is where local signed URLs are served.
	storageLocalPath = "/storage"
)

// storageConfig is the storage settings from the environment.
type storageConfig struct {
	Driver    string // STORAGE_DRIVER
	LocalDir  string // STORAGE_LOCAL_DIR
	Endpoint  string // STORAGE_ENDPOINT
	Region    string // STORAGE_REGION
	Bucket    string // STORAGE_BUCKET
	AccessKey string // STORAGE_ACCESS_KEY
	SecretKey config.Secret
	PublicURL string // STORAGE_PUBLIC_URL
	PathStyle bool   // STORAGE_PATH_STYLE
	// SigningKey signs local signed URLs (STORAGE_SIGNING_KEY); random per
	// start when empty, so those URLs stop working at a restart.
	SigningKey config.Secret
}

// loadStorageConfig reads the storage settings; production must name an
// S3-compatible driver.
func loadStorageConfig(get func(string) string, secret func(string) config.Secret, production bool) (storageConfig, []error) {
	c := storageConfig{Driver: get("STORAGE_DRIVER"), LocalDir: get("STORAGE_LOCAL_DIR"), Endpoint: get("STORAGE_ENDPOINT"), Region: get("STORAGE_REGION"),
		Bucket: get("STORAGE_BUCKET"), AccessKey: get("STORAGE_ACCESS_KEY"), SecretKey: secret("STORAGE_SECRET_KEY"), PublicURL: get("STORAGE_PUBLIC_URL"),
		SigningKey: secret("STORAGE_SIGNING_KEY")}
	var errs []error
	if c.Driver == "" {
		c.Driver = storageDriverLocal
	}
	if c.LocalDir == "" {
		c.LocalDir = filepath.Join(".orb", "storage")
	}
	switch c.Driver {
	case storageDriverLocal:
		if production {
			errs = append(errs, errors.New("STORAGE_DRIVER=local is for development; production needs s3, spaces, r2 or minio with STORAGE_ENDPOINT, STORAGE_BUCKET, STORAGE_ACCESS_KEY and STORAGE_SECRET_KEY"))
		}
	case storageDriverS3, storageDriverSpaces, storageDriverR2, storageDriverMinIO:
		if c.Endpoint == "" {
			c.Endpoint = storageDefaultEndpoint(c.Driver, c.Region)
		}
		for name, v := range map[string]string{"STORAGE_ENDPOINT": c.Endpoint, "STORAGE_BUCKET": c.Bucket, "STORAGE_ACCESS_KEY": c.AccessKey, "STORAGE_SECRET_KEY": c.SecretKey.Reveal()} {
			if v == "" {
				errs = append(errs, fmt.Errorf("%s is required when STORAGE_DRIVER=%s", name, c.Driver))
			}
		}
	default:
		errs = append(errs, fmt.Errorf("STORAGE_DRIVER must be local, s3, spaces, r2 or minio, got %q", c.Driver))
	}
	if v := get("STORAGE_PATH_STYLE"); v != "" {
		c.PathStyle = v == "true" || v == "1"
	} else {
		c.PathStyle = c.Driver == storageDriverMinIO
	}
	return c, errs
}

// storageDefaultEndpoint is the service's endpoint when the region says
// enough: Amazon S3 and Spaces; R2 and MinIO need STORAGE_ENDPOINT.
func storageDefaultEndpoint(driver, region string) string {
	switch {
	case driver == storageDriverS3 && region != "":
		return "s3." + region + ".amazonaws.com"
	case driver == storageDriverS3:
		return "s3.amazonaws.com"
	case driver == storageDriverSpaces && region != "":
		return region + ".digitaloceanspaces.com"
	}
	return ""
}

// newStorage opens the store the configuration names. For local, signed
// URLs are served at storageLocalPath by the app itself (routes.go).
func newStorage(cfg Config) (storage.Store, http.Handler, error) {
	c := cfg.Storage
	if c.Driver != storageDriverLocal {
		store, err := s3.New(s3.Config{Driver: c.Driver, Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, AccessKey: c.AccessKey, SecretKey: c.SecretKey,
			UseSSL: !strings.HasPrefix(c.Endpoint, "127.0.0.1") && !strings.HasPrefix(c.Endpoint, "localhost"), PathStyle: c.PathStyle, PublicURL: c.PublicURL})
		return store, nil, err
	}
	key := []byte(c.SigningKey.Reveal())
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, nil, err
		}
	}
	base := cfg.Social.PublicURL
	if base == devPublicURL { // the sign-in default, not where the app listens
		base = ""
	}
	if base == "" {
		host, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		base = "http://" + net.JoinHostPort(host, port)
	}
	store, err := local.New(c.LocalDir, local.WithSigner(key, strings.TrimRight(base, "/")+storageLocalPath))
	if err != nil {
		return nil, nil, err
	}
	return store, http.StripPrefix(storageLocalPath, store.Handler()), nil
}
