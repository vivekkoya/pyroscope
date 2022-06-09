package ingestion

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

type IngestionQueue struct {
	logger   logrus.FieldLogger
	ingester Ingester

	wg    sync.WaitGroup
	queue chan *IngestInput
	stop  chan struct{}

	discardedTotal prometheus.Counter
}

func NewIngestionQueue(logger logrus.FieldLogger, ingester Ingester, r prometheus.Registerer, queueWorkers, queueSize int) *IngestionQueue {
	q := IngestionQueue{
		logger:   logger,
		ingester: ingester,
		queue:    make(chan *IngestInput, queueSize),
		stop:     make(chan struct{}),

		// TODO(eh-am)
		discardedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "pyroscope_ingestion_queue_discarded_total",
			Help: "number of ingestion requests discarded",
		}),
	}

	q.wg.Add(queueWorkers)
	for i := 0; i < queueWorkers; i++ {
		go q.runQueueWorker()
	}

	return &q
}

func (s *IngestionQueue) Stop() {
	close(s.stop)
	s.wg.Wait()
}

func (s *IngestionQueue) Put(ctx context.Context, input *IngestInput) error {
	select {
	case <-ctx.Done():
	case <-s.stop:
	case s.queue <- input:
		// Once input is queued, context cancellation is ignored.
		return nil
	default:
		// Drop data if the queue is full.
	}
	s.discardedTotal.Inc()
	return nil
}

func (s *IngestionQueue) runQueueWorker() {
	defer s.wg.Done()
	for {
		select {
		case input, ok := <-s.queue:
			if ok {
				if err := s.safePut(input); err != nil {
					s.logger.WithField("key", input.Metadata.Key.Normalized()).WithError(err).Error("error happened while ingesting data")
				}
			}
		case <-s.stop:
			return
		}
	}
}

func (s *IngestionQueue) safePut(input *IngestInput) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered: %v; %v", r, string(debug.Stack()))
		}
	}()
	// TODO(kolesnikovae): It's better to derive a context that is cancelled on Stop.
	return s.ingester.Ingest(context.TODO(), input)
}