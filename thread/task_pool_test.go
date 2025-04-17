package thread

import (
	"testing"
)

func TestTask(t *testing.T) {
	tp := NewTaskPool(2, 2)
	tp.AddTask(func() {
		t.Log("Task 1")
	})
	tp.AddTask(func() {
		t.Log("Task 2")
	})
	tp.AddTask(func() {
		t.Log("Task 3")
	})
	tp.AddTask(func() {
		t.Log("Task 4")
	})
}
