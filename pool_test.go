package pool4go

import (
	"fmt"
	"os"
	"testing"
)

var p Pool

func TestMain(m *testing.M) {
	p = New(func() (Conn, error) {
		var s = &session{}
		fmt.Println("create connection")
		return s, nil
	})

	os.Exit(m.Run())
}

type session struct {
}

func (this *session) Close() error {
	return nil
}

func BenchmarkPool4go_Get(b *testing.B) {
	for i := 0; i < b.N; i++ {
		func() {
			var s, err = p.Get()
			if err != nil {
				fmt.Println(err)
			}
			if s != nil {
				p.Put(s)
			}
		}()
	}
}
