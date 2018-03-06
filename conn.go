package pool4go

import "time"

type Connection interface {
	Close() error
}

type idleConn struct {
	conn Connection
	t    time.Time
}
