package pool4go

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

type Pool struct {
	DialFunc     func() (Conn, error)
	TestOnBorrow func(conn Conn, t time.Time) error

	MaxIdle     int
	MaxActive   int
	IdleTimeout time.Duration

	active   int
	running  bool
	idleList *list.List
	mu       *sync.Mutex
	cond     *sync.Cond
}

func NewPool(dialFunc func() (Conn, error), maxIdle int) *Pool {
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

func (this *Pool) Get() (c Conn, err error) {
	c, err = this.get()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (this *Pool) get() (c Conn, err error) {
	for {
		this.mu.Lock()
		var item = this.idleList.Front()
		if item != nil && item.Value != nil {
			if idleConn, ok := item.Value.(idleConn); ok {
				this.idleList.Remove(item)
				if this.IdleTimeout > 0 && time.Now().After(idleConn.t.Add(this.IdleTimeout)) {
					this.release(idleConn.c)
					continue
				}
				if this.TestOnBorrow != nil {
					if err = this.TestOnBorrow(idleConn.c, idleConn.t); err != nil {
						this.release(idleConn.c)
						continue
					}
				}
				this.mu.Unlock()
				return idleConn.c, nil
			}
		}

		if this.running == false {
			this.mu.Unlock()
			return nil, errors.New("Get on closed pool")
		}

		if this.MaxActive <= 0 || this.active < this.MaxActive {
			c, err := this.DialFunc()
			if err != nil {
				c = nil
			}
			if c != nil {
				this.active += 1
			}
			this.mu.Unlock()
			return c, nil
		}

		this.cond.Wait()
		this.mu.Unlock()
	}
	return nil, nil
}

func (this *Pool) Release(c Conn, forceClose bool) {
	if c != nil {
		this.put(c, forceClose)
	}
}

func (this *Pool) release(c Conn) {
	if c != nil {
		c.Close()
		this.active -= 1
	}
	this.cond.Signal()
}

func (this *Pool) put(c Conn, forceClose bool) {
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.running == false || c == nil {
		return
	}

	if forceClose == false {
		this.idleList.PushFront(idleConn{t: time.Now(), c: c})
		if this.idleList.Len() > this.MaxIdle {
			c = this.idleList.Remove(this.idleList.Back()).(idleConn).c
		} else {
			c = nil
		}
	}

	this.release(c)
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
		item.Value.(idleConn).c.Close()
	}
	return nil
}

func (this *Pool) ActiveCount() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.active
}
