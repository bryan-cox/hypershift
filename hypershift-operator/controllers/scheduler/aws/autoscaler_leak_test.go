package scheduler

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/event"

	"go.uber.org/goleak"
)

func TestEnqueuePeriodicReconcile(t *testing.T) {
	currentGoroutines := goleak.IgnoreCurrent()
	g := NewWithT(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	ticks := make(chan time.Time)
	events := make(chan event.GenericEvent)
	done := make(chan error, 1)
	go func() {
		done <- enqueuePeriodicReconcile(ctx, ticks, events)
	}()

	// First prove that the worker sends a reconcile event while running.
	go func() { ticks <- time.Now() }()
	g.Eventually(events).Should(Receive(WithTransform(func(e event.GenericEvent) string {
		return e.Object.GetName()
	}, Equal("ticker"))))

	// A second tick blocks on the unbuffered event channel. Shutdown must
	// release the worker even when no controller is receiving events.
	ticks <- time.Now()
	cancel()
	g.Eventually(done).Should(Receive(BeNil()))
	g.Eventually(func() error { return goleak.Find(currentGoroutines) }).Should(Succeed())
}
