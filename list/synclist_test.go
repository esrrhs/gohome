package list

import (
	"fmt"
	"testing"
)

func Test0001(t *testing.T) {
	tt := NewList()
	tt.Push(1)
	tt.Push(11)
	tt.Push(111)
	fmt.Println(tt.Len())
	fmt.Println(tt.Pop())
	fmt.Println(tt.Len())
}

func TestListContain(t *testing.T) {
	tt := NewList()
	tt.Push(1)
	tt.Push(2)
	tt.Push(3)

	fmt.Println("Contain(2):", tt.Contain(2))
	if !tt.Contain(2) {
		t.Errorf("Contain(2) = false, want true")
	}

	fmt.Println("Contain(99):", tt.Contain(99))
	if tt.Contain(99) {
		t.Errorf("Contain(99) = true, want false")
	}

	empty := NewList()
	fmt.Println("Contain on empty list:", empty.Contain(1))
	if empty.Contain(1) {
		t.Errorf("Contain on empty list = true, want false")
	}
}

func TestListContainBy(t *testing.T) {
	tt := NewList()
	tt.Push(10)
	tt.Push(20)
	tt.Push(30)

	eqFunc := func(left interface{}, right interface{}) bool {
		return left.(int) == right.(int)
	}

	fmt.Println("ContainBy(20):", tt.ContainBy(20, eqFunc))
	if !tt.ContainBy(20, eqFunc) {
		t.Errorf("ContainBy(20) = false, want true")
	}

	fmt.Println("ContainBy(99):", tt.ContainBy(99, eqFunc))
	if tt.ContainBy(99, eqFunc) {
		t.Errorf("ContainBy(99) = true, want false")
	}
}

func TestListRange(t *testing.T) {
	tt := NewList()
	tt.Push(1)
	tt.Push(2)
	tt.Push(3)

	var collected []int
	tt.Range(func(value interface{}) {
		collected = append(collected, value.(int))
	})

	fmt.Println("Range collected:", collected)
	if len(collected) != 3 {
		t.Errorf("Range collected %d items, want 3", len(collected))
	}

	empty := NewList()
	var emptyCollected []int
	empty.Range(func(value interface{}) {
		emptyCollected = append(emptyCollected, value.(int))
	})
	fmt.Println("Range on empty list collected:", emptyCollected)
	if len(emptyCollected) != 0 {
		t.Errorf("Range on empty list collected %d items, want 0", len(emptyCollected))
	}
}
