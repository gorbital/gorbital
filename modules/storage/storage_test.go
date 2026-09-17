package storage_test

import (
	"testing"
	"time"

	"gorbital.dev/modules/storage"
)

func TestKeysAndHelpers(t *testing.T) {
	for key, want := range map[string]bool{"a": true, "a/b.txt": true, "é/ü.png": true, "": false, "/a": false, "a//b": false, "a/../b": false, "a/./b": false, "a\x00": false, "a\n": false} {
		if got := storage.ValidKey(key); got != want {
			t.Errorf("ValidKey(%q) = %v, want %v", key, got, want)
		}
	}
	if !storage.ValidPrefix("") || !storage.ValidPrefix("a/") || storage.ValidPrefix("/a/") {
		t.Error("ValidPrefix")
	}
	if storage.DirectoryMarker("docs/") != "docs/.keep" || !storage.IsDirectoryMarker("docs/.keep") || storage.IsDirectoryMarker("docs/keep") {
		t.Error("directory markers")
	}
	if storage.ClampExpiry(0) != time.Hour || storage.ClampExpiry(time.Millisecond) != time.Second || storage.ClampExpiry(30*24*time.Hour) != storage.MaxSignedURLExpiry {
		t.Error("ClampExpiry")
	}
	if storage.ContentTypeFor("a/B.PNG") != "image/png" || storage.ContentTypeFor("x.unknown") != "application/octet-stream" || storage.ContentTypeFor("dir.d/file") != "application/octet-stream" {
		t.Error("ContentTypeFor")
	}
	if !storage.ValidSignedMethod("GET") || !storage.ValidSignedMethod("PUT") || storage.ValidSignedMethod("DELETE") {
		t.Error("ValidSignedMethod")
	}
}
