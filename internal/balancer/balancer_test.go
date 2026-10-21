package balancer

import (
	"fmt"
	"testing"

	"github.com/Hrishikesh16nikam/go-loadbalancer/internal/backend"
)

func makeBackends(t *testing.T, n int) []*backend.Backend {
	t.Helper()
	out := make([]*backend.Backend, n)
	for i := range out {
		b, err := backend.New(fmt.Sprintf("b%d", i), fmt.Sprintf("127.0.0.1:%d", 9000+i))
		if err != nil {
			t.Fatal(err)
		}
		out[i] = b
	}
	return out
}

func TestRoundRobinOrder(t *testing.T) {
	bs := makeBackends(t, 3)
	rr := &RoundRobin{}
	for round := 0; round < 2; round++ {
		for i, want := range bs {
			if got := rr.Next(bs); got != want {
				t.Fatalf("round %d pick %d: got %s, want %s", round, i, got.Name, want.Name)
			}
		}
	}
}

func TestRoundRobinIsEven(t *testing.T) {
	bs := makeBackends(t, 3)
	rr := &RoundRobin{}
	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		counts[rr.Next(bs).Name]++
	}
	for _, b := range bs {
		if counts[b.Name] != 100 {
			t.Errorf("%s got %d picks, want 100", b.Name, counts[b.Name])
		}
	}
}

func TestLeastConnectionsPicksFewestActive(t *testing.T) {
	bs := makeBackends(t, 3)
	bs[0].Acquire()
	bs[0].Acquire()
	bs[1].Acquire()
	lc := &LeastConnections{}
	if got := lc.Next(bs); got != bs[2] {
		t.Fatalf("got %s, want b2", got.Name)
	}
	bs[2].Acquire()
	bs[2].Acquire()
	if got := lc.Next(bs); got != bs[1] {
		t.Fatalf("got %s, want b1", got.Name)
	}
}

func TestEmptyCandidates(t *testing.T) {
	if (&RoundRobin{}).Next(nil) != nil {
		t.Error("round robin should return nil for no candidates")
	}
	if (&LeastConnections{}).Next(nil) != nil {
		t.Error("least connections should return nil for no candidates")
	}
}

func TestNew(t *testing.T) {
	for _, name := range []string{RoundRobinName, LeastConnectionsName} {
		b, err := New(name)
		if err != nil {
			t.Fatalf("New(%q): %v", name, err)
		}
		if b.Name() != name {
			t.Errorf("New(%q).Name() = %q", name, b.Name())
		}
	}
	if _, err := New("random"); err == nil {
		t.Error("expected an error for an unknown algorithm")
	}
}
