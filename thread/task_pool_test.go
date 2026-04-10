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

func TestTaskPool_Stats(t *testing.T) {
tp := NewTaskPool(2, 10)

// Add some tasks and wait for them to complete
for i := 0; i < 5; i++ {
tp.AddTask(func() {})
}

done := tp.DoneNum()
t.Logf("DoneNum: %d", done)
if done < 5 {
t.Errorf("expected DoneNum >= 5, got %d", done)
}

tp.ResetDoneNum()
if tp.DoneNum() != 0 {
t.Errorf("expected DoneNum 0 after reset, got %d", tp.DoneNum())
}

taskNum := tp.TaskNum()
t.Logf("TaskNum: %d", taskNum)

sleepNum := tp.SleepNum()
t.Logf("SleepNum: %d", sleepNum)

tp.ResetSleepNum()
t.Logf("SleepNum after reset: %d", tp.SleepNum())
}
