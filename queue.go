package autodelete

import (
	"container/heap"
	"fmt"
	mrand "math/rand"
	"strings"
	"sync"
	"time"
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
	ch   *ManagedChannel
	msgs []string
}

type reapQueue struct {
	items  *priorityQueue
	cond   *sync.Cond
	timer  *time.Timer
	workCh chan reapWorkItem

	curMu   sync.Mutex
	curWork map[*ManagedChannel]struct{}
}

func newReapQueue() *reapQueue {
	var locker sync.Mutex
	q := &reapQueue{
		items:   new(priorityQueue),
		cond:    sync.NewCond(&locker),
		timer:   time.NewTimer(0),
		workCh:  make(chan reapWorkItem),
		curWork: make(map[*ManagedChannel]struct{}),
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

// Update adds or inserts the expiry time for the given item in the queue.
func (q *reapQueue) Update(ch *ManagedChannel, t time.Time) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	idx := -1
	for i, v := range *q.items {
		if v.ch == ch {
			idx = i
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

func (q *reapQueue) WaitForNext() *ManagedChannel {
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
		goto start
	}
	x := heap.Pop(q.items)
	q.cond.L.Unlock()
	it = x.(*pqItem)
	return it.ch
}

func (b *Bot) QueueReap(c *ManagedChannel) {
	reapTime := c.GetNextDeletionTime()
	b.reaper.Update(c, reapTime)
}

func (b *Bot) QueueLoadBacklog(c *ManagedChannel, didFail bool) {
	c.mu.Lock()
	loadDelay := c.loadFailures
	if didFail {
		c.loadFailures = time.Duration(int64(loadDelay)*2 + int64(mrand.Intn(int(5*time.Second))))
		loadDelay = c.loadFailures
	}
	c.mu.Unlock()

	b.loadRetries.Update(c, time.Now().Add(loadDelay))
}

func reapScheduler(q *reapQueue, numWorkers int, workerFunc func(*reapQueue)) {
	for i := 0; i < numWorkers; i++ {
		go workerFunc(q)
	}

	for {
		ch := q.WaitForNext()

		q.curMu.Lock()
		_, channelAlreadyBeingProcessed := q.curWork[ch]
		if !channelAlreadyBeingProcessed {
			q.curWork[ch] = struct{}{}
		}
		q.curMu.Unlock()

		if channelAlreadyBeingProcessed {
			continue
		}

		q.workCh <- reapWorkItem{ch: ch}
	}
}

func (b *Bot) loadWorker(q *reapQueue) {
	for work := range q.workCh {
		ch := work.ch

		err := ch.LoadBacklog()

		q.curMu.Lock()
		delete(q.curWork, ch)
		q.curMu.Unlock()

		if isRetryableLoadError(err) {
			b.QueueLoadBacklog(ch, true)
		}
	}
}

func (b *Bot) reapWorker(q *reapQueue) {
	for work := range q.workCh {
		ch := work.ch
		msgs := ch.collectMessagesToDelete()

		fmt.Printf("[reap] %s #%s: deleting %d messages\n", ch.Channel.ID, ch.Channel.Name, len(msgs))
		count, err := ch.Reap(msgs)
		if b.handleCriticalPermissionsErrors(ch.Channel.ID, err) {
			continue
		}
		if err != nil {
			fmt.Printf("[reap] %s #%s: deleted %d, got error: %v\n", ch.Channel.ID, ch.Channel.Name, count, err)
			b.QueueLoadBacklog(ch, false)
		} else if count == -1 {
			fmt.Printf("[reap] %s #%s: doing single-message delete\n", ch.Channel.ID, ch.Channel.Name)
		}

		q.curMu.Lock()
		delete(q.curWork, ch)
		q.curMu.Unlock()
		b.QueueReap(ch)
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
