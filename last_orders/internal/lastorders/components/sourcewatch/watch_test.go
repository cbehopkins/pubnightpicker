package sourcewatch

import (
	"context"
	"sync/atomic"
	"testing"
)

type testStream struct {
	stops atomic.Int32
}

func (stream *testStream) Stop() {
	stream.stops.Add(1)
}

func TestWatchStopCancelsAndStopsStreamOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &testStream{}
	watch := New(cancel, stream)
	watch.Stop()
	watch.Stop()

	if ctx.Err() != context.Canceled {
		t.Fatalf("watch context error=%v, want canceled", ctx.Err())
	}
	if got := stream.stops.Load(); got != 1 {
		t.Fatalf("stream Stop calls=%d, want 1", got)
	}
}
