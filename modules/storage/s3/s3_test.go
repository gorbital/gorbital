package s3_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/modules/storage"
	"gorbital.dev/modules/storage/s3"
)

// TestS3Store runs against an S3-compatible service named by
// GORBITAL_TEST_S3_ENDPOINT, _BUCKET, _ACCESS_KEY and _SECRET_KEY (a local
// MinIO, for example); it is skipped otherwise.
func TestS3Store(t *testing.T) {
	endpoint, bucket := os.Getenv("GORBITAL_TEST_S3_ENDPOINT"), os.Getenv("GORBITAL_TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("set GORBITAL_TEST_S3_ENDPOINT, _BUCKET, _ACCESS_KEY and _SECRET_KEY to test the S3 driver")
	}
	ctx := context.Background()
	s, err := s3.New(s3.Config{Driver: "minio", Endpoint: endpoint, Bucket: bucket, AccessKey: os.Getenv("GORBITAL_TEST_S3_ACCESS_KEY"),
		SecretKey: config.NewSecret(os.Getenv("GORBITAL_TEST_S3_SECRET_KEY")), UseSSL: os.Getenv("GORBITAL_TEST_S3_SSL") == "1", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	prefix := "gorbital-test/" + time.Now().Format("20060102150405") + "/"
	key := prefix + "hello.txt"
	if _, err := s.Put(ctx, key, strings.NewReader("hello"), 5, storage.PutOptions{Metadata: map[string]string{"owner": "ada"}}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Delete(ctx, key) }()
	o, err := s.Stat(ctx, key)
	if err != nil || o.Size != 5 || o.Metadata["owner"] != "ada" {
		t.Errorf("stat = %+v, %v", o, err)
	}
	r, _, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r)
	r.Close()
	if string(body) != "hello" {
		t.Errorf("get = %q", body)
	}
	page, err := s.List(ctx, storage.ListOptions{Prefix: prefix})
	if err != nil || len(page.Objects) != 1 {
		t.Errorf("list = %+v, %v", page, err)
	}
	if u, err := s.SignedURL(ctx, key, "GET", time.Minute); err != nil || !strings.Contains(u, "X-Amz-Signature") {
		t.Errorf("signed = %q, %v", u, err)
	}
	moved, err := storage.Move(ctx, s, key, prefix+"moved.txt")
	if err != nil || moved.Size != 5 {
		t.Errorf("move = %+v, %v", moved, err)
	}
	defer func() { _ = s.Delete(ctx, prefix+"moved.txt") }()
	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("after move = %v", err)
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := s3.New(s3.Config{Endpoint: "s3.amazonaws.com", Bucket: "b"}); err == nil {
		t.Error("New without credentials succeeded")
	}
	if _, err := s3.New(s3.Config{Bucket: "b", AccessKey: "a", SecretKey: config.NewSecret("s")}); err == nil {
		t.Error("New without an endpoint succeeded")
	}
}
