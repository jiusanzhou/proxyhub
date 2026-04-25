package session

import (
	"testing"
	"time"

	"github.com/jiusanzhou/proxyhub/internal/pool"
)

func TestManagerBindAndGet(t *testing.T) {
	m := NewManager(10*time.Minute, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80", Country: "CN"}
	m.Bind("sess1", p, 0)

	s := m.Get("sess1")
	if s == nil {
		t.Fatal("session not found")
	}
	if s.Proxy.URL != p.URL {
		t.Errorf("wrong proxy: %v", s.Proxy)
	}
}

func TestManagerTTL(t *testing.T) {
	m := NewManager(50*time.Millisecond, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80"}
	m.Bind("s1", p, 50*time.Millisecond)

	if m.Get("s1") == nil {
		t.Fatal("should exist")
	}
	time.Sleep(100 * time.Millisecond)
	if m.Get("s1") != nil {
		t.Error("should be expired")
	}
}

func TestManagerFailureRotation(t *testing.T) {
	m := NewManager(10*time.Minute, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80"}
	m.Bind("s1", p, 0)

	// 1,2 次失败：不该轮转
	if m.RecordFailure("s1") {
		t.Error("should not rotate on first fail")
	}
	if m.RecordFailure("s1") {
		t.Error("should not rotate on 2nd fail")
	}
	// 第 3 次：达到阈值
	if !m.RecordFailure("s1") {
		t.Error("should rotate on 3rd fail")
	}
}

func TestManagerTouchResetsFailures(t *testing.T) {
	m := NewManager(10*time.Minute, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80"}
	m.Bind("s1", p, 0)

	m.RecordFailure("s1")
	m.RecordFailure("s1")
	m.Touch("s1")
	// 重置后再失败两次不应触发
	if m.RecordFailure("s1") {
		t.Error("failure count should reset after Touch")
	}
}

func TestManagerCleanup(t *testing.T) {
	m := NewManager(50*time.Millisecond, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80"}
	m.Bind("alive", p, 10*time.Minute)
	m.Bind("expire", p, 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	n := m.Cleanup()
	if n != 1 {
		t.Errorf("want 1 cleaned, got %d", n)
	}
	if m.Size() != 1 {
		t.Errorf("want size=1, got %d", m.Size())
	}
}

func TestManagerRotate(t *testing.T) {
	m := NewManager(10*time.Minute, 3)
	p := &pool.Proxy{URL: "http://1.1.1.1:80"}
	m.Bind("s1", p, 0)

	m.Rotate("s1")
	if m.Get("s1") != nil {
		t.Error("session should be gone after rotate")
	}
}
