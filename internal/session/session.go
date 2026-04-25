// Package session 管理 session-based 粘性 IP
//
// 主流付费代理服务（Bright Data / SmartProxy / Oxylabs）的标准范式：
//   - 客户端传 session ID（header 或 username）
//   - 同一 session 绑定同一出口 IP
//   - 失败时自动换 IP（内部透明）
//   - TTL 过期后回收
//
// proxyhub 兼容这套语义。
package session

import (
	"sync"
	"time"

	"go.zoe.im/proxyhub/internal/pool"
)

// Session 粘性会话
type Session struct {
	ID        string
	Proxy     *pool.Proxy
	CreatedAt time.Time
	LastUsed  time.Time
	TTL       time.Duration
	// FailureCount 当前绑定代理累计失败次数，>=MaxFailures 触发轮转
	FailureCount int
}

// IsExpired 是否过期
func (s *Session) IsExpired(now time.Time) bool {
	return now.Sub(s.LastUsed) > s.TTL
}

// Manager 会话管理器（内存态，重启丢失）
type Manager struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	defaultTTL time.Duration
	maxFailures int
}

// NewManager 创建会话管理器
func NewManager(defaultTTL time.Duration, maxFailures int) *Manager {
	if defaultTTL <= 0 {
		defaultTTL = 10 * time.Minute
	}
	if maxFailures <= 0 {
		maxFailures = 3
	}
	return &Manager{
		sessions:    make(map[string]*Session),
		defaultTTL:  defaultTTL,
		maxFailures: maxFailures,
	}
}

// Get 获取指定 session；不存在或过期返回 nil
func (m *Manager) Get(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	if s.IsExpired(time.Now()) {
		return nil
	}
	return s
}

// Bind 给 session 绑定一个代理（新建或重绑）
func (m *Manager) Bind(id string, p *pool.Proxy, ttl time.Duration) *Session {
	if ttl <= 0 {
		ttl = m.defaultTTL
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &Session{
		ID:        id,
		Proxy:     p,
		CreatedAt: now,
		LastUsed:  now,
		TTL:       ttl,
	}
	m.sessions[id] = s
	return s
}

// Touch 更新 LastUsed（请求成功时调用）
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.LastUsed = time.Now()
		s.FailureCount = 0 // 成功后重置失败计数
	}
}

// RecordFailure 记录会话失败；返回是否触发轮转
func (m *Manager) RecordFailure(id string) (shouldRotate bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return false
	}
	s.FailureCount++
	return s.FailureCount >= m.maxFailures
}

// Rotate 强制解绑 session 的代理（下次 Get 会挑新代理）
func (m *Manager) Rotate(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Cleanup 清理过期 session
func (m *Manager) Cleanup() int {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for id, s := range m.sessions {
		if s.IsExpired(now) {
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}

// Size 当前活跃 session 数
func (m *Manager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// MaxFailures 返回轮转阈值
func (m *Manager) MaxFailures() int {
	return m.maxFailures
}

// All 返回所有活跃 session 快照（调试用）
func (m *Manager) All() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}
