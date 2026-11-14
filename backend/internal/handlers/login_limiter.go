package handlers

import (
	"sync"
	"time"

	"one-mail/backend/internal/access"
)

const (
	loginFailureLimit  = 10
	loginFailureWindow = time.Minute
)

// One instance-wide window avoids trusting forwarded addresses and keeps memory
// constant even when clients rotate addresses. The cooldown precedes password checks.
type loginFailureLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	window   time.Time
	failures int
}

func newLoginFailureLimiter() *loginFailureLimiter {
	return &loginFailureLimiter{now: time.Now}
}

func (l *loginFailureLimiter) recordFailure() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resetWindow()
	if retryAfter := l.retryAfterLocked(); retryAfter > 0 {
		return retryAfter
	}
	if l.failures == 0 {
		l.window = l.now()
	}
	l.failures++
	return 0
}

func (l *loginFailureLimiter) retryAfter() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resetWindow()
	return l.retryAfterLocked()
}

// Hold the lock through password verification to enforce the same budget for
// concurrent attempts; request bodies are read before acquiring this lock.
func (l *loginFailureLimiter) login(instanceAccess *access.Access, password string) (string, error, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resetWindow()
	if retryAfter := l.retryAfterLocked(); retryAfter > 0 {
		return "", nil, retryAfter
	}
	token, err := instanceAccess.Login(password)
	if err != nil {
		if l.failures == 0 {
			l.window = l.now()
		}
		l.failures++
	}
	return token, err, 0
}

func (l *loginFailureLimiter) resetWindow() {
	now := l.now()
	if !l.window.IsZero() && !now.Before(l.window.Add(loginFailureWindow)) {
		l.window = time.Time{}
		l.failures = 0
	}
}

func (l *loginFailureLimiter) retryAfterLocked() int {
	if l.failures < loginFailureLimit {
		return 0
	}
	remaining := l.window.Add(loginFailureWindow).Sub(l.now())
	return int((remaining + time.Second - 1) / time.Second)
}
