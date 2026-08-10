package metrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
)

func TestNewRejectsInvalidServeMuxPattern(t *testing.T) {
	_, err := New(&Config{
		ServiceName: "invalid-pattern-test",
		Port:        9090,
		Path:        "/{",
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestGaugeStateAndRecordAreLinearized(t *testing.T) {
	tests := []struct {
		name       string
		first      func(*gaugeImpl)
		second     func(*gaugeImpl)
		wantRecord []float64
	}{
		{
			name:       "set then increment",
			first:      func(g *gaugeImpl) { g.Set(context.Background(), 10) },
			second:     func(g *gaugeImpl) { g.Inc(context.Background()) },
			wantRecord: []float64{10, 11},
		},
		{
			name:       "increment then decrement",
			first:      func(g *gaugeImpl) { g.Inc(context.Background()) },
			second:     func(g *gaugeImpl) { g.Dec(context.Background()) },
			wantRecord: []float64{1, 0},
		},
		{
			name:       "decrement then set",
			first:      func(g *gaugeImpl) { g.Dec(context.Background()) },
			second:     func(g *gaugeImpl) { g.Set(context.Background(), 10) },
			wantRecord: []float64{-1, 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := newBlockingFloat64Gauge()
			gauge := &gaugeImpl{g: recorder, values: make(map[string]float64)}

			firstDone := make(chan struct{})
			go func() {
				defer close(firstDone)
				tt.first(gauge)
			}()
			<-recorder.firstEntered

			secondStarted := make(chan struct{})
			secondDone := make(chan struct{})
			go func() {
				close(secondStarted)
				defer close(secondDone)
				tt.second(gauge)
			}()
			<-secondStarted

			select {
			case <-secondDone:
				t.Fatal("second update completed before the first Record returned")
			case <-time.After(100 * time.Millisecond):
			}

			close(recorder.releaseFirst)
			<-firstDone
			<-secondDone

			if got := recorder.recordedValues(); !equalFloat64s(got, tt.wantRecord) {
				t.Fatalf("recorded values = %v, want %v", got, tt.wantRecord)
			}
		})
	}
}

type blockingFloat64Gauge struct {
	metric.Float64Gauge

	firstEntered chan struct{}
	releaseFirst chan struct{}

	mu      sync.Mutex
	calls   int
	records []float64
}

func newBlockingFloat64Gauge() *blockingFloat64Gauge {
	return &blockingFloat64Gauge{
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
}

func (g *blockingFloat64Gauge) Record(_ context.Context, value float64, _ ...metric.RecordOption) {
	g.mu.Lock()
	g.calls++
	call := g.calls
	g.mu.Unlock()

	if call == 1 {
		close(g.firstEntered)
		<-g.releaseFirst
	}

	g.mu.Lock()
	g.records = append(g.records, value)
	g.mu.Unlock()
}

func (g *blockingFloat64Gauge) recordedValues() []float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]float64(nil), g.records...)
}

func equalFloat64s(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
