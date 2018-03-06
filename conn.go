package pool4go

import "time"

type Conn interface {
	Close() error
}

type idleConn struct {
	c Conn
	t time.Time
}
