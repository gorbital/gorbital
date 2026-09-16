package local_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorbital.dev/modules/storage"
	"gorbital.dev/modules/storage/local"
)

func TestLocalStore(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir(), local.WithSigner([]byte("secret"), "http://127.0.0.1:8080/storage"))
	if err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); info.Driver != "local" || !info.Local || info.Endpoint == "" {
		t.Errorf("info = %+v", info)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	obj, err := s.Put(ctx, "docs/hello.txt", strings.NewReader("hello world"), -1, storage.PutOptions{Metadata: map[string]string{"Owner": "ada"}})
	if err != nil {
		t.Fatal(err)
	}
	if obj.Size != 11 || obj.ContentType != "text/plain; charset=utf-8" || obj.ETag == "" || obj.Metadata["owner"] != "ada" {
		t.Errorf("put = %+v", obj)
	}
	for _, key := range []string{"../x", "/abs", "a//b", "a/./b", "", strings.Repeat("k", 1025)} {
		if _, err := s.Put(ctx, key, strings.NewReader("x"), 1, storage.PutOptions{}); !errors.Is(err, storage.ErrInvalidKey) {
			t.Errorf("Put(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := s.Put(ctx, "img/a.png", strings.NewReader("png"), 3, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, "docs/sub/deep.md", strings.NewReader("# deep"), 6, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(ctx, storage.DirectoryMarker("empty"), strings.NewReader(""), 0, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Stat(ctx, "docs/hello.txt")
	if err != nil || got.Size != 11 || got.Metadata["owner"] != "ada" || got.ETag != obj.ETag {
		t.Errorf("stat = %+v, %v", got, err)
	}
	if _, err := s.Stat(ctx, "docs/nope.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat missing = %v", err)
	}
	if _, err := s.Stat(ctx, "docs"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("stat a directory = %v", err)
	}
	r, _, err := s.Get(ctx, "docs/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r)
	r.Close()
	if string(body) != "hello world" {
		t.Errorf("get = %q", body)
	}

	page, err := s.List(ctx, storage.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 0 || strings.Join(page.Prefixes, ",") != "docs/,empty/,img/" || page.NextCursor != "" {
		t.Errorf("root list = %+v", page)
	}
	page, _ = s.List(ctx, storage.ListOptions{Prefix: "docs/"})
	if len(page.Objects) != 1 || page.Objects[0].Key != "docs/hello.txt" || strings.Join(page.Prefixes, ",") != "docs/sub/" {
		t.Errorf("docs list = %+v", page)
	}
	page, _ = s.List(ctx, storage.ListOptions{Recursive: true, Limit: 2})
	if len(page.Objects) != 2 || page.NextCursor == "" {
		t.Errorf("recursive page 1 = %+v", page)
	}
	// Recursive listings include directory markers, so a folder can be
	// emptied from the API.
	page2, _ := s.List(ctx, storage.ListOptions{Recursive: true, Limit: 2, Cursor: page.NextCursor})
	if len(page2.Objects) != 2 || page2.Objects[0].Key != "empty/.keep" || page2.Objects[1].Key != "img/a.png" || page2.NextCursor != "" {
		t.Errorf("recursive page 2 = %+v", page2)
	}

	moved, err := storage.Move(ctx, s, "docs/hello.txt", "archive/hello.txt")
	if err != nil || moved.Key != "archive/hello.txt" || moved.Metadata["owner"] != "ada" {
		t.Errorf("move = %+v, %v", moved, err)
	}
	if _, err := s.Stat(ctx, "docs/hello.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Error("source still exists after move")
	}
	if err := s.Delete(ctx, "archive/hello.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "archive/hello.txt"); err != nil {
		t.Errorf("delete twice = %v", err)
	}
	page, _ = s.List(ctx, storage.ListOptions{})
	if strings.Contains(strings.Join(page.Prefixes, ","), "archive") {
		t.Errorf("empty directory kept: %+v", page)
	}

	// Signed URLs: served by the handler, only while valid and for the
	// signed method.
	u, err := s.SignedURL(ctx, "img/a.png", http.MethodGet, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.StripPrefix("/storage", s.Handler()))
	defer srv.Close()
	u = strings.Replace(u, "http://127.0.0.1:8080", srv.URL, 1)
	res, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "png" || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("signed GET = %d %q %s", res.StatusCode, body, res.Header.Get("Content-Type"))
	}
	if res, _ := http.Get(strings.Replace(u, "sig=", "sig=0", 1)); res.StatusCode != http.StatusForbidden {
		t.Errorf("tampered signature = %d", res.StatusCode)
	}
	if res, _ := http.Get(strings.Replace(u, "img/a.png", "docs/sub/deep.md", 1)); res.StatusCode != http.StatusForbidden {
		t.Errorf("other key with the same signature = %d", res.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPut, u, strings.NewReader("x"))
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusForbidden {
		t.Errorf("PUT with a GET signature = %d", res.StatusCode)
	}
	up, _ := s.SignedURL(ctx, "uploads/new.txt", http.MethodPut, time.Minute)
	req, _ = http.NewRequest(http.MethodPut, strings.Replace(up, "http://127.0.0.1:8080", srv.URL, 1), strings.NewReader("uploaded"))
	req.Header.Set("Content-Type", "text/plain")
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 200 {
		t.Errorf("signed PUT = %v %d", err, res.StatusCode)
	}
	if o, err := s.Stat(ctx, "uploads/new.txt"); err != nil || o.Size != 8 {
		t.Errorf("uploaded = %+v, %v", o, err)
	}
	if _, err := s.SignedURL(ctx, "x.txt", http.MethodDelete, time.Minute); err == nil {
		t.Error("DELETE was signed")
	}
	plain, _ := local.New(t.TempDir())
	if _, err := plain.SignedURL(ctx, "x.txt", http.MethodGet, time.Minute); err == nil {
		t.Error("signed without a signer")
	}
}
