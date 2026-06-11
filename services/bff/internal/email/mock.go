package email

import (
	"context"
	"sync"
)

// MockSender is a test double for Sender.  It records all calls and can
// simulate send errors via InjectSendError.
//
// The zero value is ready to use — no initialisation required.
// All methods are safe for concurrent use.
type MockSender struct {
	mu sync.Mutex

	sendErr error

	completeCalls []string // recipient addresses
	failedCalls   []string // recipient addresses
}

// InjectSendError causes both Send* methods to return err on their next call
// (and all subsequent calls until reset).
func (m *MockSender) InjectSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErr = err
}

// SendDeletionComplete records the call and returns any injected error.
func (m *MockSender) SendDeletionComplete(_ context.Context, toEmail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCalls = append(m.completeCalls, toEmail)
	return m.sendErr
}

// SendDeletionFailed records the call and returns any injected error.
func (m *MockSender) SendDeletionFailed(_ context.Context, toEmail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedCalls = append(m.failedCalls, toEmail)
	return m.sendErr
}

// DeletionCompleteCallCount returns the number of SendDeletionComplete calls.
func (m *MockSender) DeletionCompleteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.completeCalls)
}

// DeletionFailedCallCount returns the number of SendDeletionFailed calls.
func (m *MockSender) DeletionFailedCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.failedCalls)
}

// LastDeletionCompleteAddr returns the most-recent recipient passed to
// SendDeletionComplete, or the empty string if it was never called.
func (m *MockSender) LastDeletionCompleteAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.completeCalls) == 0 {
		return ""
	}
	return m.completeCalls[len(m.completeCalls)-1]
}

// LastDeletionFailedAddr returns the most-recent recipient passed to
// SendDeletionFailed, or the empty string if it was never called.
func (m *MockSender) LastDeletionFailedAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.failedCalls) == 0 {
		return ""
	}
	return m.failedCalls[len(m.failedCalls)-1]
}
