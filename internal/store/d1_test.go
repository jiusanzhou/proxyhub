// D1 store 集成测试
//
// 仅在以下环境变量齐全时运行：
//
//	CF_ACCOUNT_ID    Cloudflare Account ID
//	CF_D1_DATABASE_ID  D1 Database ID
//	CF_API_TOKEN     Cloudflare API Token（需要 D1:Edit 权限）
package store

import (
	"context"
	"os"
	"testing"

	"go.zoe.im/proxyhub/internal/pool"
)

func TestD1StoreIntegration(t *testing.T) {
	accountID := os.Getenv("CF_ACCOUNT_ID")
	databaseID := os.Getenv("CF_D1_DATABASE_ID")
	apiToken := os.Getenv("CF_API_TOKEN")

	if accountID == "" || databaseID == "" || apiToken == "" {
		t.Skip("skipping D1 integration test: set CF_ACCOUNT_ID, CF_D1_DATABASE_ID, CF_API_TOKEN")
	}

	ctx := context.Background()
	s, err := NewD1(ctx, D1Config{
		AccountID:  accountID,
		DatabaseID: databaseID,
		APIToken:   apiToken,
	})
	if err != nil {
		t.Fatalf("NewD1: %v", err)
	}
	defer s.Close()

	// Save
	proxies := []*pool.Proxy{
		{URL: "http://1.2.3.4:8080", Protocol: pool.ProtoHTTP, Country: "US", Source: "test"},
		{URL: "socks5://5.6.7.8:1080", Protocol: pool.ProtoSOCKS5, Country: "DE", Source: "test"},
	}
	if err := s.Save(ctx, proxies); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// LoadAll
	loaded, err := s.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) < 2 {
		t.Errorf("expected >= 2 proxies, got %d", len(loaded))
	}

	// 验证其中一条
	found := false
	for _, p := range loaded {
		if p.URL == "http://1.2.3.4:8080" {
			found = true
			if p.Country != "US" {
				t.Errorf("country mismatch: got %s", p.Country)
			}
		}
	}
	if !found {
		t.Error("saved proxy not found in LoadAll result")
	}
}
