package cache

import (
	"testing"
	"time"
)

func TestGetMiss(t *testing.T) {
	c := New[string, int](0)
	if e := c.Get("x"); e != nil {
		t.Fatal("expected nil for missing key")
	}
}

func TestPutAndGet(t *testing.T) {
	c := New[string, string](0)
	c.Put("k", "v")
	e := c.Get("k")
	if e == nil || e.Value != "v" {
		t.Fatalf("expected v, got %v", e)
	}
}

func TestPutOverwrite(t *testing.T) {
	c := New[string, int](0)
	c.Put("k", 1)
	c.Put("k", 2)
	e := c.Get("k")
	if e == nil || e.Value != 2 {
		t.Fatalf("expected 2, got %v", e)
	}
}

func TestDelete(t *testing.T) {
	c := New[string, int](0)
	c.Put("k", 1)
	c.Delete("k")
	if e := c.Get("k"); e != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestLRUEviction(t *testing.T) {
	c := New[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // should evict "a"
	if e := c.Get("a"); e != nil {
		t.Fatal("expected 'a' to be evicted")
	}
	if e := c.Get("b"); e == nil {
		t.Fatal("expected 'b' to survive")
	}
	if e := c.Get("c"); e == nil {
		t.Fatal("expected 'c' to survive")
	}
}

func TestLRUTouchPreventsEviction(t *testing.T) {
	c := New[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a")    // touch "a", making "b" the oldest
	c.Put("c", 3) // should evict "b"
	if e := c.Get("a"); e == nil {
		t.Fatal("expected 'a' to survive after touch")
	}
	if e := c.Get("b"); e != nil {
		t.Fatal("expected 'b' to be evicted")
	}
}

func TestAgeMiss(t *testing.T) {
	c := New[string, int](0)
	if age := c.Age("x"); age != -1 {
		t.Fatalf("expected -1 for missing key, got %v", age)
	}
}

func TestAgeHit(t *testing.T) {
	c := New[string, int](0)
	c.Put("k", 1)
	time.Sleep(5 * time.Millisecond)
	age := c.Age("k")
	if age < 5*time.Millisecond {
		t.Fatalf("expected age >= 5ms, got %v", age)
	}
}

func TestStructKey(t *testing.T) {
	type Key struct {
		A string
		B int
	}
	c := New[Key, string](0)
	c.Put(Key{"x", 1}, "val")
	e := c.Get(Key{"x", 1})
	if e == nil || e.Value != "val" {
		t.Fatalf("expected val for struct key, got %v", e)
	}
	if e := c.Get(Key{"x", 2}); e != nil {
		t.Fatal("expected nil for different struct key")
	}
}
