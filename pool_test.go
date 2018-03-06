package pool4go

import (
	"testing"
	"fmt"
)

type session struct {

}

func (this *session) Close() error {
	return nil
}

func BenchmarkPool4go_Get(b *testing.B) {
	var p = NewPool(func() (Connection, error) {
		var s = &session{}
		fmt.Println("create connection")
		return s, nil
	}, 10)
	p.MaxActive = 15

	for i:=0; i<b.N; i++ {
		var s, err = p.Get()
		if err != nil {
			fmt.Println(err)
		}
		if s != nil {
			p.Release(s, false)
		}
	}
}