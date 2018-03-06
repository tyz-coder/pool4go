package pool4go

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

type Pool struct {
	DialFunc     func() (Connection, error)
	TestOnBorrow func(conn Connection, t time.Time) error

	MaxIdle     int
	MaxActive   int
	IdleTimeout time.Duration

	active   int
	running  bool
	idleList *list.List
	mu       *sync.Mutex
	cond     *sync.Cond
}

func NewPool(dialFunc func() (Connection, error), maxIdle int) *Pool {
	var p = &Pool{}
	p.DialFunc = dialFunc
	p.MaxIdle = maxIdle
	p.MaxActive = maxIdle + 5
	p.active = 0
	p.running = true
	p.idleList = list.New()
	p.mu = &sync.Mutex{}
	p.cond = sync.NewCond(p.mu)
	return p
}

func (this *Pool) Get() (conn Connection, err error) {
	conn, err = this.get()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (this *Pool) get() (conn Connection, err error) {
	for {
		this.mu.Lock()
		var item = this.idleList.Front()
		if item != nil && item.Value != nil {
			if conn, ok := item.Value.(idleConn); ok {
				this.idleList.Remove(item)
				if this.IdleTimeout > 0 && time.Now().After(conn.t.Add(this.IdleTimeout)) {
					conn.conn.Close()
					this.release()
					continue
				}
				if this.TestOnBorrow != nil {
					if err = this.TestOnBorrow(conn.conn, conn.t); err != nil {
						conn.conn.Close()
						this.release()
						continue
					}
				}
				this.mu.Unlock()
				return conn.conn, nil
			}
		}

		if this.running == false {
			return nil, errors.New("Get on closed pool")
		}

		if this.MaxActive <= 0 || this.active < this.MaxActive {
			conn, err := this.DialFunc()
			if err != nil {
				conn = nil
			}
			if conn != nil {
				this.active += 1
			}
			this.mu.Unlock()
			return conn, nil
		}

		this.cond.Wait()
		this.mu.Unlock()
	}
	return nil, nil
}

func (this *Pool) Release(conn Connection, forceClose bool) {
	if conn != nil {
		this.put(conn, forceClose)
	}
}

func (this *Pool) release() {
	this.active -= 1
	this.cond.Signal()
}

func (this *Pool) put(conn Connection, forceClose bool) {
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.running == false || conn == nil {
		return
	}

	if forceClose == false {
		this.idleList.PushFront(idleConn{t: time.Now(), conn: conn})
		if this.idleList.Len() > this.MaxIdle {
			conn = this.idleList.Remove(this.idleList.Back()).(idleConn).conn
		} else {
			conn = nil
		}
	}

	if conn == nil {
		if this.cond != nil {
			this.cond.Signal()
		}
		return
	}

	conn.Close()
	this.release()
}

func (this *Pool) Close() error {
	this.mu.Lock()
	if this.running == false {
		return nil
	}
	this.running = false
	this.active = 0
	this.cond.Broadcast()
	var idle = this.idleList
	this.idleList.Init()
	this.mu.Unlock()

	for item := idle.Front(); item != nil; item = item.Next() {
		item.Value.(idleConn).conn.Close()
	}
	return nil
}

func (this *Pool) ActiveCount() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.active
}
