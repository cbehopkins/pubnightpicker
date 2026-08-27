package cellar_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"testing/synctest"
	"time"

	"cellar/pkg/cellar"
	cellarsqlite "cellar/pkg/sqlite"
)

type greeting struct {
	Name string `json:"name"`
}

type greetingHandler struct {
	messages chan<- string
}

func (h greetingHandler) Handle(ctx context.Context, payload greeting) cellar.Result {
	h.messages <- "Hello, " + payload.Name + "!"
	return cellar.Complete{}
}

func Example_helloworld() {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: 10 * time.Millisecond})
	messages := make(chan string, 1)

	if err := runtime.Register("hello.greet", greetingHandler{messages: messages}); err != nil {
		panic(err)
	}
	if _, err := runtime.Add("hello.greet", greeting{Name: "Cellar"}); err != nil {
		panic(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()

	fmt.Println(<-messages)
	if err := runtime.Stop(); err != nil {
		panic(err)
	}
	if err := <-done; err != nil {
		panic(err)
	}

	// Output:
	// Hello, Cellar!
}

func ExampleCellar_AddSequence() {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	messages := make(chan string, 2)

	if err := runtime.Register("hello.greet", greetingHandler{messages: messages}); err != nil {
		panic(err)
	}
	if err := runtime.Register("hello.speak", greetingHandler{messages: messages}); err != nil {
		panic(err)
	}
	if _, err := runtime.AddSequence(
		cellar.Step{HandlerName: "hello.greet", Payload: greeting{Name: "Cellar"}},
		cellar.Step{HandlerName: "hello.speak", Payload: greeting{Name: "World"}},
	); err != nil {
		panic(err)
	}

	done := make(chan error, 1)
	go func() { done <- runtime.Start(context.Background()) }()

	fmt.Println(<-messages)
	fmt.Println(<-messages)
	if err := runtime.Stop(); err != nil {
		panic(err)
	}
	if err := <-done; err != nil {
		panic(err)
	}

	// Output:
	// Hello, Cellar!
	// Hello, World!
}

type orderCompleted struct {
	OrderID string `json:"order_id"`
	Email   string `json:"email"`
}

type sendEmail struct {
	OrderID string `json:"order_id"`
	Email   string `json:"email"`
}

type publishAnalytics struct {
	OrderID string `json:"order_id"`
}

type fanoutHandler[T any] struct {
	name     string
	messages chan<- string
}

func (h fanoutHandler[T]) Handle(ctx context.Context, payload T) cellar.Result {
	h.messages <- fmt.Sprintf("%s: %+v", h.name, payload)
	return cellar.Complete{}
}

func ExampleFanout() {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond, Workers: 2})
	messages := make(chan string, 2)

	if err := runtime.Register("email.send", fanoutHandler[sendEmail]{name: "email", messages: messages}); err != nil {
		panic(err)
	}
	if err := runtime.Register("analytics.publish", fanoutHandler[publishAnalytics]{name: "analytics", messages: messages}); err != nil {
		panic(err)
	}

	fanout, err := cellar.NewFanout("order.completed", cellar.FanoutExpanderFunc[orderCompleted](
		func(ctx context.Context, parentID cellar.CellID, order orderCompleted) ([]cellar.FanoutTarget, error) {
			return []cellar.FanoutTarget{
				{Key: "email", HandlerName: "email.send", Payload: sendEmail{OrderID: order.OrderID, Email: order.Email}},
				{Key: "analytics", HandlerName: "analytics.publish", Payload: publishAnalytics{OrderID: order.OrderID}},
			}, nil
		},
	))
	if err != nil {
		panic(err)
	}
	if err := fanout.Register(runtime); err != nil {
		panic(err)
	}
	if _, err := fanout.Add(runtime, orderCompleted{OrderID: "order-42", Email: "person@example.com"}); err != nil {
		panic(err)
	}

	done := make(chan error, 1)
	go func() { done <- runtime.Start(context.Background()) }()
	output := []string{<-messages, <-messages}
	if err := runtime.Stop(); err != nil {
		panic(err)
	}
	if err := <-done; err != nil {
		panic(err)
	}

	sort.Strings(output)
	for _, message := range output {
		fmt.Println(message)
	}

	// Output:
	// analytics: {OrderID:order-42}
	// email: {OrderID:order-42 Email:person@example.com}
}

const timerHandlerName cellar.HandlerName = "timer.tick"

func Example_timer() {
	const interval = 5 * time.Second

	directory, err := os.MkdirTemp("", "cellar-timer-example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)

	databasePath := filepath.Join(directory, "timer.db")
	store, err := cellarsqlite.Open(databasePath, nil)
	if err != nil {
		panic(err)
	}
	// Phase 1: Create everything the first time
	// make sure we have a timer that runs after an interval
	runtime := cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	runs := make(chan time.Time, 2)
	timer, err := cellar.NewTimer(timerHandlerName, cellar.TimerConfig{
		Interval: interval,
		Mode:     cellar.TimerFixedDelay,
	}, func(context.Context) error {
		runs <- time.Now()
		return nil
	})
	if err != nil {
		panic(err)
	}
	if err := timer.Register(runtime); err != nil {
		panic(err)
	}
	if _, err := timer.Schedule(runtime); err != nil {
		panic(err)
	}
	active, err := store.ListActive()
	if err != nil || len(active) != 1 || active[0].NotBefore == nil {
		panic(err)
	}
	firstDue := *active[0].NotBefore

	done := make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()

	firstRun := <-runs
	if err := runtime.Stop(); err != nil {
		panic(err)
	}
	if err := <-done; err != nil {
		panic(err)
	}
	if err := store.Close(); err != nil {
		panic(err)
	}
	// Phase 2: reopen the store and runtime, and verify that the timer runs again after the interval.
	store, err = cellarsqlite.Open(databasePath, nil)
	if err != nil {
		panic(err)
	}
	defer store.Close()
	runtime = cellar.New(store, cellar.Config{PollDelay: time.Millisecond})
	timer, err = cellar.NewTimer(timerHandlerName, cellar.TimerConfig{
		Interval: interval,
		Mode:     cellar.TimerFixedDelay,
	}, func(context.Context) error {
		runs <- time.Now()
		return nil
	})
	if err != nil {
		panic(err)
	}
	if err := timer.Register(runtime); err != nil {
		panic(err)
	}

	done = make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()

	secondRun := <-runs
	fmt.Println("first run was not early:", !firstRun.Before(firstDue))
	fmt.Println("persisted timer ran after reopening:", !secondRun.Before(firstRun.Add(interval)))

	if err := runtime.Stop(); err != nil {
		panic(err)
	}
	if err := <-done; err != nil {
		panic(err)
	}
}

func TestExampleTimerTiming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 5 * time.Second

		databasePath := filepath.Join(t.TempDir(), "timer.db")
		store, err := cellarsqlite.Open(databasePath, nil)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		runtime := cellar.New(store, cellar.Config{PollDelay: time.Second})
		runs := make(chan time.Time, 2)
		timer, err := cellar.NewTimer(timerHandlerName, cellar.TimerConfig{
			Interval: interval,
			Mode:     cellar.TimerFixedDelay,
		}, func(context.Context) error {
			runs <- time.Now()
			return nil
		})
		if err != nil {
			t.Fatalf("NewTimer() error = %v", err)
		}
		if err := timer.Register(runtime); err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		if _, err := timer.Schedule(runtime); err != nil {
			t.Fatalf("Schedule() error = %v", err)
		}
		active, err := store.ListActive()
		if err != nil {
			t.Fatalf("ListActive() error = %v", err)
		}
		if len(active) != 1 || active[0].NotBefore == nil {
			t.Fatalf("active timers = %#v, want one scheduled timer", active)
		}
		firstDue := *active[0].NotBefore

		done := make(chan error, 1)
		go func() {
			done <- runtime.Start(t.Context())
		}()
		synctest.Wait()

		assertTimerHasNotRun(t, runs)
		time.Sleep(interval - time.Nanosecond)
		synctest.Wait()
		assertTimerHasNotRun(t, runs)

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		firstRun := receiveTimerRun(t, runs)
		if firstRun.Before(firstDue) {
			t.Fatalf("first run at %v, before due time %v", firstRun, firstDue)
		}

		if err := runtime.Stop(); err != nil {
			t.Fatalf("first Stop() error = %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("first Start() error = %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("first Close() error = %v", err)
		}

		store, err = cellarsqlite.Open(databasePath, nil)
		if err != nil {
			t.Fatalf("reopen store error = %v", err)
		}
		defer store.Close()
		runtime = cellar.New(store, cellar.Config{PollDelay: time.Second})
		timer, err = cellar.NewTimer(timerHandlerName, cellar.TimerConfig{
			Interval: interval,
			Mode:     cellar.TimerFixedDelay,
		}, func(context.Context) error {
			runs <- time.Now()
			return nil
		})
		if err != nil {
			t.Fatalf("NewTimer() after reopen error = %v", err)
		}
		if err := timer.Register(runtime); err != nil {
			t.Fatalf("Register() after reopen error = %v", err)
		}
		done = make(chan error, 1)
		go func() {
			done <- runtime.Start(t.Context())
		}()
		synctest.Wait()

		secondDue := firstRun.Add(interval)
		time.Sleep(interval - time.Nanosecond)
		synctest.Wait()
		assertTimerHasNotRun(t, runs)

		time.Sleep(time.Nanosecond)
		synctest.Wait()
		secondRun := receiveTimerRun(t, runs)
		if secondRun.Before(secondDue) {
			t.Fatalf("second run at %v, before due time %v", secondRun, secondDue)
		}

		if err := runtime.Stop(); err != nil {
			t.Fatalf("second Stop() error = %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("second Start() error = %v", err)
		}
	})
}

func assertTimerHasNotRun(t *testing.T, runs <-chan time.Time) {
	t.Helper()
	select {
	case ranAt := <-runs:
		t.Fatalf("timer ran early at %v", ranAt)
	default:
	}
}

func receiveTimerRun(t *testing.T, runs <-chan time.Time) time.Time {
	t.Helper()
	select {
	case ranAt := <-runs:
		return ranAt
	default:
		t.Fatal("timer did not run when due")
		return time.Time{}
	}
}
