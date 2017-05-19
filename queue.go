package autodelete

import (
	"container/heap"
	"fmt"
	"sync"
	"time"
)

// An Item is something we manage in a priority queue.
type pqItem struct {
	ch *ManagedChannel
	nextReap time.Time    // The priority of the item in the queue.
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

type reapQueue struct {
	items *priorityQueue
	cond  *sync.Cond
	timer *time.Timer
}

func newReapQueue() *reapQueue {
	var locker sync.Mutex
	q := &reapQueue{
		items: new(priorityQueue),
		cond: sync.NewCond(&locker),
		timer: time.NewTimer(0),
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
			ch: ch,
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
		q.cond.Wait()
		goto start
	}
	now := time.Now()
	if it.nextReap.After(now) {
		go q.timer.Reset(it.nextReap.Sub(now) + 2*time.Millisecond)
		q.cond.Wait()
		goto start
	}
	x := heap.Pop(q.items)
	q.cond.L.Unlock()
	it = x.(*pqItem)
	return it.ch
}

func (b *Bot) QueueReap(c *ManagedChannel) {
	var reapTime time.Time

	reapTime = c.GetNextDeletionTime(reapTime)
	fmt.Println("got reap queue for", c.Channel.ID, c.Channel.Name, reapTime)
	b.reaper.Update(c, reapTime)
}

func (b *Bot) reapWorker() {
	for {
		ch := b.reaper.WaitForNext()
		fmt.Printf("Reaper starting for %s\n", ch.Channel.ID)
		err := ch.Reap()
		if err != nil {
			fmt.Printf("Reaper error for %s: %v\n", ch.Channel.ID, err)
		}
	}
}
