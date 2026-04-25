package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jiusanzhou/proxyhub/internal/pool"
)

func TestSQLiteRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	p := &pool.Proxy{
		URL:      "http://1.2.3.4:8080",
		Host:     "1.2.3.4",
		Port:     8080,
		Protocol: pool.ProtoHTTP,
		Country:  "CN",
		Source:   "test",
	}
	p.SetStats(10, 8, 2, 500_000)

	ctx := context.Background()
	if err := st.Save(ctx, []*pool.Proxy{p}); err != nil {
		t.Fatal(err)
	}

	loaded, err := st.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("want 1 proxy, got %d", len(loaded))
	}
	if loaded[0].URL != p.URL || loaded[0].SuccessCount() != 8 {
		t.Errorf("loaded mismatch: %+v", loaded[0])
	}
}
