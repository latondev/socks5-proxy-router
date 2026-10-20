package router

import (
	"sync"
	"time"

	"socks5-pool/internal/model"
)

// LogStore holds recent activity events in thread-safe memory.
type LogStore struct {
	mu   sync.RWMutex
	logs []model.ActivityLog
}

func NewLogStore(capacity int) *LogStore {
	return &LogStore{
		logs: make([]model.ActivityLog, 0, capacity),
	}
}

func (s *LogStore) Add(msg, logType string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	vnLoc := time.FixedZone("ICT", 7*3600)
	tStr := time.Now().In(vnLoc).Format("15:04:05")
	entry := model.ActivityLog{Time: tStr, Message: msg, Type: logType}

	s.logs = append([]model.ActivityLog{entry}, s.logs...)
	if len(s.logs) > 10 {
		s.logs = s.logs[:10]
	}
}

func (s *LogStore) All() []model.ActivityLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]model.ActivityLog, len(s.logs))
	copy(result, s.logs)
	return result
}
