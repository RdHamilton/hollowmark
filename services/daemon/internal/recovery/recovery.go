// Package recovery provides a deferred panic-recovery helper for long-lived
// daemon goroutines. Every recovered panic is logged with a structured message
// and forwarded to Sentry via a caller-supplied capture function so no panic
// is ever swallowed silently.
//
// Usage — call as the first defer in the goroutine body:
//
//	go func() {
//	    defer recovery.RecoverGoroutine("poller", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
//	    // ... goroutine body
//	}()
//
// The capture function receives a non-nil *error* value whose text is the
// string representation of the original panic value (or the error itself when
// the panic value already implements error). Restart logic is NOT included in
// the helper — that separation of concerns belongs to the owning service.
package recovery

import (
	"fmt"
	"log"
	"runtime/debug"
)

// CaptureFunc is the signature of a Sentry (or test-double) exception capture
// function. Its return value is intentionally void so the recovery package
// stays free of sentry-go imports. Use SentryCaptureFunc to adapt
// sentry.CurrentHub().CaptureException (which returns *sentry.EventID).
type CaptureFunc func(err error)

// CaptureFn adapts any function of the form func(error) T (where T is
// discarded) into a CaptureFunc. Use this to pass sentry.CurrentHub().CaptureException,
// which returns *sentry.EventID, without a type mismatch:
//
//	defer recovery.RecoverGoroutine("poller", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
func CaptureFn[T any](fn func(error) T) CaptureFunc {
	return func(err error) { fn(err) }
}

// RecoverGoroutine is a deferred helper for long-lived daemon goroutines.
// It must be called via defer as the first statement in the goroutine body.
//
// On a panic it:
//  1. Logs "[daemon] goroutine panic name=<name> err=<err>" at the standard logger.
//  2. Logs the full stack trace so operators can locate the source site.
//  3. Calls capture(err) so the event reaches Sentry (calls on a nil SDK are safe
//     no-ops per the sentry-go contract; the SDK is initialized by sentryhook.Init).
//
// On a clean (non-panic) exit it is a no-op.
func RecoverGoroutine(name string, capture CaptureFunc) {
	r := recover()
	if r == nil {
		return
	}

	var err error
	switch v := r.(type) {
	case error:
		err = v
	default:
		err = fmt.Errorf("%v", v)
	}

	stack := debug.Stack()
	log.Printf("[daemon] goroutine panic name=%s err=%v\nstack:\n%s", name, err, stack)

	if capture != nil {
		capture(err)
	}
}
