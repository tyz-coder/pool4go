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

	numOpenConn int
	maxIdleConn int
	maxOpenConn int
	IdleTimeout time.Duration

	running  bool
	idleList *list.List
	mu       *sync.Mutex
	cond     *sync.Cond
}

const (
	k_DEFAULT_MAX_IDLE_CONN = 2
)

func NewPool(dialFunc func() (Conn, error)) *Pool {
	var p = &Pool{}
	p.DialFunc = dialFunc
	p.maxIdleConn = k_DEFAULT_MAX_IDLE_CONN
	p.maxOpenConn = k_DEFAULT_MAX_IDLE_CONN
	p.numOpenConn = 0
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
			if idleConn, ok := item.Value.(*idleConn); ok {
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

		if this.maxOpenConn <= 0 || this.numOpenConn < this.maxOpenConn {
			c, err := this.DialFunc()
			if err != nil {
				c = nil
			}
			if c != nil {
				this.numOpenConn += 1
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
		this.numOpenConn -= 1
	}
	this.cond.Signal()
}

func (this *Pool) put(c Conn, forceClose bool) {
	this.mu.Lock()
	defer this.mu.Unlock()

	if c == nil {
		return
	}

	if this.running == false {
		this.release(c)
		return
	}

	if forceClose == false {
		this.idleList.PushFront(&idleConn{t: time.Now(), c: c})
		if this.idleList.Len() > this.maxIdleConn {
			c = this.idleList.Remove(this.idleList.Back()).(*idleConn).c
		} else {
			c = nil
		}
	}

	this.release(c)
}

func (this *Pool) Close() error {
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.running == false {
		return nil
	}
	this.running = false
	this.numOpenConn = 0
	this.cond.Broadcast()
	for item := this.idleList.Front(); item != nil; item = item.Next() {
		item.Value.(*idleConn).c.Close()
	}
	this.idleList.Init()
	return nil
}

func (this *Pool) NumOpenConns() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.numOpenConn
}

func (this *Pool) SetMaxIdleConns(n int) {
	this.mu.Lock()
	defer this.mu.Unlock()

	if n <= 0 {
		this.maxIdleConn = 0
	} else {
		this.maxIdleConn = n
	}

	if this.maxOpenConn > 0 && this.maxIdleConn > this.maxOpenConn {
		this.maxIdleConn = this.maxOpenConn
	}

	for {
		if this.idleList.Len() <= this.maxIdleConn {
			this.cond.Signal()
			return
		}

		var c = this.idleList.Remove(this.idleList.Back()).(*idleConn).c
		if c != nil {
			c.Close()
			this.numOpenConn -= 1
		}
	}
}

func (this *Pool) MaxIdleConns() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.maxIdleConn
}

func (this *Pool) SetMaxOpenConns(n int) {
	this.mu.Lock()
	this.maxOpenConn = n
	if n <= 0 {
		this.maxOpenConn = 0
	}
	syncMaxIdle := this.maxOpenConn > 0 && this.maxIdleConn > this.maxOpenConn
	this.mu.Unlock()
	if syncMaxIdle {
		this.SetMaxIdleConns(this.maxOpenConn)
	}
}

func (this *Pool) MaxOpenConns() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.maxOpenConn
}
