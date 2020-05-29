package autodelete

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	schedulerTimeout = 250 * time.Millisecond
	workerTimeout    = 5 * time.Second
	maxLoadBackoff   = 30 * time.Minute

	labelQueue = "queue"
	queueReap  = "reap"
	queueLoad  = "load"
)

// Quality of service for the load queues. Lower numbers are higher priority.
//
// The reuse of the reap queue for backlog load ordering is a hack - this should really be an ordered list of FIFO queues - but it's easier to just reuse the code. Plus, we get "wait until timestamp" for QOSLoadError for "free".
type LoadQOS int8

const (
	QOSInteractive         LoadQOS = iota // Modify command
	QOSNewMessage                         // Saw message in channel
	QOSInitNoPins                         // Bot start events
	QOSLargeDelete                        // Deleted >25 messages
	QOSSingleMessageDelete                // Saw extremely old messages
	QOSInitWithPins                       // Start event with LPTS
	QOSLoadError                          // Previous attempt error

	QOSInvalid
	QOSInit = QOSInitWithPins
)

var qosEpoch = time.Now().Add(-300 * time.Hour)

func (q LoadQOS) ApplyBackoff() bool {
	return q == QOSSingleMessageDelete || q == QOSLoadError
}

func (q LoadQOS) Upgrade(to LoadQOS) LoadQOS {
	if q < to {
		return q
	}
	return to
}

func (q LoadQOS) Time() time.Time {
	return qosEpoch.Add(time.Duration(int(q)))
}

var (
	mReapqLen = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_total",
		Help:      "number of items in reapq",
	}, []string{"queue"})
	mReapqWorkerCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_worker_total",
		Help:      "number of workers in reapq",
	}, []string{"queue"})
	mReapqInFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_inflight_total",
		Help:      "number of work items currently being processed",
	}, []string{"queue"})
	mReapqWaitDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_wait_seconds",
		Help:      "seconds slept between queue items",
		Buckets:   bucketsDeletionTimes,
	}, []string{"queue"})
	mReapqE2eLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_latency_e2e_seconds",
		Help:      "delay between when a queue item was due and when it actually begun processing",
		Buckets:   bucketsDeletionTimes,
	}, []string{"queue"})
	mReapqUpdate = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_updates",
		Help:      "number of times an item is inserted or updated in the queue",
	}, []string{"queue"})
	mReapqWorkerStart = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_worker_start_total",
		Help:      "number of times a new worker is started",
	}, []string{"queue"})
	mReapqWorkerStop = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_worker_stop_total",
		Help:      "number of times a worker is halted",
	}, []string{"queue"})
	mReapqDropChannel = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: nsAutodelete,
		Name:      "reapq_drop_channel_total",
		Help:      "times that a channel picked up was marked as disabled",
	}, []string{"queue"})

	reapqMetricsC = []*prometheus.CounterVec{
		mReapqUpdate,
		mReapqWorkerStart,
		mReapqWorkerStop,
		mReapqDropChannel,
	}
)

// An Item is something we manage in a priority queue.
type pqItem struct {
	ch       *ManagedChannel
	nextReap time.Time // The priority of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// A priorityQueue implements heap.Interface and holds Items.
type priorityQueue []*pqItem

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return pq[i].nextReap.Before(pq[j].nextReap)
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*pqItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

func (pq priorityQueue) Peek() *pqItem {
	if len(pq) == 0 {
		return nil
	}
	return pq[0]
}

type reapWorkItem struct {
	ch  *ManagedChannel
	due time.Time
}

type workerToken struct{}

type reapQueue struct {
	items  *priorityQueue
	cond   *sync.Cond
	timer  *time.Timer
	label  string
	workCh chan reapWorkItem

	// Send when a worker starts, receive when a worker quits
	controlCh chan workerToken

	curMu   sync.Mutex
	curWork map[*ManagedChannel]struct{}
}

func newReapQueue(maxWorkerCount int, label string) *reapQueue {
	var locker sync.Mutex
	q := &reapQueue{
		items:     new(priorityQueue),
		cond:      sync.NewCond(&locker),
		timer:     time.NewTimer(0),
		label:     label,
		workCh:    make(chan reapWorkItem),
		controlCh: make(chan workerToken, maxWorkerCount),
		curWork:   make(map[*ManagedChannel]struct{}),
	}
	go func() {
		// Signal the condition variable every time the timer expires.
		for {
			<-q.timer.C
			q.cond.Signal()
		}
	}()
	heap.Init(q.items)
	return q
}

type reapqCollector struct {
	qs []*reapQueue
}

func (c reapqCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, v := range reapqMetricsC {
		v.Describe(ch)
	}

	mReapqLen.Describe(ch)
	mReapqWorkerCount.Describe(ch)
	mReapqInFlight.Describe(ch)
	mReapqWaitDuration.Describe(ch)
	mReapqE2eLatency.Describe(ch)
}

func (c reapqCollector) Collect(ch chan<- prometheus.Metric) {
	for _, q := range c.qs {
		q.cond.L.Lock()
		mReapqLen.WithLabelValues(q.label).Set(float64(len(*q.items)))
		mReapqWorkerCount.WithLabelValues(q.label).Set(float64(len(q.controlCh)))
		q.cond.L.Unlock()
		q.curMu.Lock()
		mReapqInFlight.WithLabelValues(q.label).Set(float64(len(q.curWork)))
		q.curMu.Unlock()
	}

	for _, v := range reapqMetricsC {
		v.Collect(ch)
	}

	mReapqLen.Collect(ch)
	mReapqWorkerCount.Collect(ch)
	mReapqInFlight.Collect(ch)
	mReapqWaitDuration.Collect(ch)
	mReapqE2eLatency.Collect(ch)
}

// Update adds or inserts the expiry time for the given item in the queue.
func (q *reapQueue) Update(ch *ManagedChannel, t time.Time) {
	mReapqUpdate.WithLabelValues(q.label).Inc()

	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	idx := -1
	for i, v := range *q.items {
		if v.ch.ChannelID == ch.ChannelID {
			idx = i
			(*q.items)[i].ch = ch
			break
		}
	}
	if idx == -1 {
		heap.Push(q.items, &pqItem{
			ch:       ch,
			nextReap: t,
		})
	} else {
		(*q.items)[idx].nextReap = t
		heap.Fix(q.items, idx)
	}
	q.cond.Signal()
}

func (q *reapQueue) WaitForNext() (*ManagedChannel, time.Time) {
	q.cond.L.Lock()
start:
	it := q.items.Peek()
	if it == nil {
		fmt.Println("[reap] waiting for insertion")
		q.cond.Wait()
		goto start
	}
	now := time.Now()
	if it.nextReap.After(now) {
		waitTime := it.nextReap.Sub(now)
		fmt.Println("[reap] sleeping for ", waitTime-(waitTime%time.Second))
		q.timer.Reset(waitTime + 2*time.Millisecond)
		q.cond.Wait()
		actualWait := time.Now().Sub(now)
		mReapqWaitDuration.WithLabelValues(q.label).Observe(float64(actualWait) / float64(time.Second))
		goto start
	}
	x := heap.Pop(q.items)
	q.cond.L.Unlock()
	it = x.(*pqItem)
	return it.ch, it.nextReap
}

func (b *Bot) QueueReap(c *ManagedChannel) {
	reapTime := c.GetNextDeletionTime()
	b.reaper.Update(c, reapTime)
}

// Removes the given channel from the reaper, assuming that IsDisabled() will
// return true for the passed ManagedChannel.
func (b *Bot) CancelReap(c *ManagedChannel) {
	var zeroTime time.Time
	b.reaper.Update(c, zeroTime)
}

// Queue up work to reload the backlog of every channel.
//
// We do this by straight-up replacing the queue with a brand new heap, because
// there's no point in preserving the old entries if we're just doing
// everything over again.
func (b *Bot) LoadAllBacklogs() {
	now := time.Now()

	b.mu.RLock()
	newQueue := make(priorityQueue, 0, len(b.channels))
	for _, c := range b.channels {
		if c == nil {
			continue
		}
		newQueue = append(newQueue, &pqItem{ch: c, nextReap: now, index: len(newQueue)})
		now = now.Add(time.Nanosecond)
	}
	b.mu.RUnlock()

	heap.Init(&newQueue)

	b.loadRetries.cond.L.Lock()
	b.loadRetries.items = &newQueue
	b.loadRetries.cond.Signal()
	b.loadRetries.cond.L.Unlock()
}

func (b *Bot) QueueLoadBacklog(c *ManagedChannel, qos LoadQOS) {
	queuePosition := qos.Time()

	if qos.ApplyBackoff() {
		c.mu.Lock()
		loadDelay := c.loadFailures
		loadDelay = time.Duration(int64(loadDelay)*2 + int64(time.Duration(mrand.Intn(int(5*time.Second/time.Microsecond)))*time.Microsecond))
		if loadDelay >= maxLoadBackoff {
			loadDelay = maxLoadBackoff
		}
		c.loadFailures = loadDelay
		queuePosition = time.Now().Add(loadDelay)

		c.mu.Unlock()
	}

	b.loadRetries.Update(c, queuePosition)
}

func reapScheduler(q *reapQueue, workerFunc func(*reapQueue, bool)) {
	q.controlCh <- workerToken{}
	go workerFunc(q, false)

	timer := time.NewTimer(0)

	for {
		ch, due := q.WaitForNext()

		q.curMu.Lock()
		_, channelAlreadyBeingProcessed := q.curWork[ch]
		if !channelAlreadyBeingProcessed {
			q.curWork[ch] = struct{}{}
		}
		q.curMu.Unlock()

		if channelAlreadyBeingProcessed {
			continue
		}

		sendWorkItem(q, workerFunc, timer, reapWorkItem{ch: ch, due: due})
	}
}

func sendWorkItem(q *reapQueue, workerFunc func(*reapQueue, bool), timer *time.Timer, work reapWorkItem) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
			// BUG: got false from timer.Stop but nothing waiting in the channel
			fmt.Println("[BUG ] sendWorkItem got false from timer.Stop but no value to recv.")
		}
	}
	timer.Reset(schedulerTimeout)

	select {
	case q.workCh <- work:
		return
	case <-timer.C:
		// Timer expired; all workers busy. Attempt to start a new worker, or block if we're maxed
		timer.Reset(0) // prime a value for the next call
		select {
		case q.controlCh <- workerToken{}:
			fmt.Printf("[reap] %p: starting new worker\n", q)
			mReapqWorkerStart.WithLabelValues(q.label).Inc()
			go workerFunc(q, true)
			q.workCh <- work
			return
		case q.workCh <- work:
			return
		}
	}
}

func (b *Bot) loadWorker(q *reapQueue, mayTimeout bool) {
	timer := time.NewTimer(0)

	if mayTimeout {
		defer func() {
			<-q.controlCh // remove a worker token
			fmt.Printf("[reap] %p: worker exiting\n", q)
			mReapqWorkerStop.WithLabelValues(q.label).Inc()
		}()
	}

	for {
		if mayTimeout {
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(workerTimeout)
		}

		select {
		case <-timer.C:
			return
		case work := <-q.workCh:
			ch := work.ch
			if ch.IsDisabled() {
				continue
			}

			err := ch.LoadBacklog()

			q.curMu.Lock()
			delete(q.curWork, ch)
			q.curMu.Unlock()

			if isRetryableLoadError(err) {
				b.QueueLoadBacklog(ch, QOSLoadError)
			}
		}
	}
}

func (b *Bot) reapWorker(q *reapQueue, mayTimeout bool) {
	// TODO: implement mayTimeout
	for work := range q.workCh {
		ch, due := work.ch, work.due
		msgs, shouldQueueBacklog, isDisabled := ch.collectMessagesToDelete()
		if isDisabled {
			mReapqDropChannel.WithLabelValues(q.label).Inc()
			continue // drop ch
		}

		start := time.Now()
		startLatency := start.Sub(due)
		mReapqE2eLatency.WithLabelValues(q.label).Observe(float64(startLatency) / float64(time.Second))

		fmt.Printf("[reap] %s: deleting %d messages\n", ch, len(msgs))
		count, err := ch.Reap(msgs)
		if b.handleCriticalPermissionsErrors(ch.ChannelID, err) {
			continue // drop ch
		}
		if err != nil {
			fmt.Printf("[reap] %s: deleted %d, got error: %v\n", ch, count, err)
			shouldQueueBacklog = true
		} else if count == -1 {
			fmt.Printf("[reap] %s: doing single-message delete\n", ch)
			shouldQueueBacklog = false
		}

		q.curMu.Lock()
		delete(q.curWork, ch)
		q.curMu.Unlock()
		b.QueueReap(ch)
		if shouldQueueBacklog {
			b.QueueLoadBacklog(ch, QOSLargeDelete)
		}
	}
}

func isRetryableLoadError(err error) bool {
	if err == nil {
		return false
	}
	// Only error to retry is a CloudFlare HTML 429
	if strings.Contains(err.Error(), "rate limit unmarshal error") {
		return true
	}
	return false
}
