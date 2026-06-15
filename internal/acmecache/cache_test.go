package acmecache

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/acme/autocert"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func testDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("open+migrate test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := New(testDB(t))

	// Miss on absent key.
	if _, err := c.Get(ctx, "missing"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("Get(missing) err = %v, want ErrCacheMiss", err)
	}

	// Put then Get.
	want := []byte("cert-bytes-\x00\x01\x02")
	if err := c.Put(ctx, "sp.example.com", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := c.Get(ctx, "sp.example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("Get = %q, want %q", got, want)
	}

	// Overwrite.
	want2 := []byte("renewed-cert")
	if err := c.Put(ctx, "sp.example.com", want2); err != nil {
		t.Fatalf("Put overwrite: %v", err)
	}
	got, err = c.Get(ctx, "sp.example.com")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if string(got) != string(want2) {
		t.Fatalf("Get after overwrite = %q, want %q", got, want2)
	}

	// Delete then miss.
	if err := c.Delete(ctx, "sp.example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Get(ctx, "sp.example.com"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("Get after delete err = %v, want ErrCacheMiss", err)
	}

	// Delete of absent key is not an error.
	if err := c.Delete(ctx, "never-existed"); err != nil {
		t.Fatalf("Delete(absent): %v", err)
	}
}

func TestCacheSatisfiesAutocert(t *testing.T) {
	var _ autocert.Cache = New(testDB(t))
}
