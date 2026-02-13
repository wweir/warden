package gateway

import "testing"

func TestMap(t *testing.T) {
	data := []int{1, 2, 3}
	result := Map(data, func(x int) int { return x * 2 })
	expected := []int{2, 4, 6}
	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("Expected %d, got %d", expected[i], result[i])
		}
	}
}

func TestFilter(t *testing.T) {
	data := []int{1, 2, 3, 4}
	result := Filter(data, func(x int) bool { return x%2 == 0 })
	expected := []int{2, 4}
	for i := range result {
		if result[i] != expected[i] {
			t.Errorf("Expected %d, got %d", expected[i], result[i])
		}
	}
}

func TestFind(t *testing.T) {
	data := []int{1, 2, 3}
	val, ok := Find(data, func(x int) bool { return x > 2 })
	if !ok {
		t.Error("Expected to find value > 2")
	}
	if val != 3 {
		t.Errorf("Expected 3, got %d", val)
	}
}

func TestReduce(t *testing.T) {
	data := []int{1, 2, 3}
	sum := Reduce(data, func(acc int, x int) int { return acc + x }, 0)
	if sum != 6 {
		t.Errorf("Expected 6, got %d", sum)
	}
}

func TestOption(t *testing.T) {
	// Some test
	opt := Some(42)
	if !opt.IsSome() || opt.IsNone() {
		t.Error("Option should be Some")
	}
	if opt.Unwrap() != 42 {
		t.Errorf("Expected 42, got %v", opt.Unwrap())
	}

	// None test
	none := None[int]()
	if none.IsSome() || !none.IsNone() {
		t.Error("Option should be None")
	}
	if none.Unwrap() != 0 {
		t.Errorf("Expected 0, got %v", none.Unwrap())
	}

	if none.UnwrapOr(10) != 10 {
		t.Errorf("Expected 10, got %v", none.UnwrapOr(10))
	}
}

func TestResult(t *testing.T) {
	// Ok test
	res := Ok[int, string](42)
	if !res.IsOk() || res.IsErr() {
		t.Error("Result should be Ok")
	}
	if res.Unwrap() != 42 {
		t.Errorf("Expected 42, got %v", res.Unwrap())
	}

	// Err test
	errRes := Err[int]("something went wrong")
	if errRes.IsOk() || !errRes.IsErr() {
		t.Error("Result should be Err")
	}

	if errRes.UnwrapOr(10) != 10 {
		t.Errorf("Expected 10, got %v", errRes.UnwrapOr(10))
	}
}

func TestConcreteSubject_RegisterObserver(t *testing.T) {
	subject := &ConcreteSubject{}

	observer1 := &TestObserver{}
	subject.RegisterObserver(observer1)
	if len(subject.observers) != 1 {
		t.Error("Expected 1 observer, got", len(subject.observers))
	}

	observer2 := &TestObserver{}
	subject.RegisterObserver(observer2)
	if len(subject.observers) != 2 {
		t.Error("Expected 2 observers, got", len(subject.observers))
	}
}

func TestConcreteSubject_RemoveObserver(t *testing.T) {
	subject := &ConcreteSubject{}

	observer1 := &TestObserver{}
	observer2 := &TestObserver{}
	subject.RegisterObserver(observer1)
	subject.RegisterObserver(observer2)

	subject.RemoveObserver(observer1)
	if len(subject.observers) != 1 {
		t.Error("Expected 1 observer, got", len(subject.observers))
	}

	subject.RemoveObserver(observer2)
	if len(subject.observers) != 0 {
		t.Error("Expected 0 observers, got", len(subject.observers))
	}
}

func TestConcreteSubject_NotifyObservers(t *testing.T) {
	subject := &ConcreteSubject{}
	observer1 := &TestObserver{}
	observer2 := &TestObserver{}
	subject.RegisterObserver(observer1)
	subject.RegisterObserver(observer2)

	data := "test notification"
	subject.NotifyObservers(data)

	if observer1.LastData != data {
		t.Errorf("Expected %v, got %v", data, observer1.LastData)
	}
	if observer2.LastData != data {
		t.Errorf("Expected %v, got %v", data, observer2.LastData)
	}
}

type TestObserver struct {
	LastData interface{}
}

func (o *TestObserver) Update(data interface{}) {
	o.LastData = data
}

func TestCommandPattern(t *testing.T) {
	receiver := &Receiver{}
	command := &ConcreteCommand{receiver}
	invoker := &Invoker{}
	invoker.SetCommand(command)

	invoker.Execute()
}
