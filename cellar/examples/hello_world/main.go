package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"cellar/pkg/cellar"
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

func main() {
	store := cellar.NewMemoryStore(nil)
	runtime := cellar.New(store, cellar.Config{PollDelay: 10 * time.Millisecond})
	messages := make(chan string, 1)

	if err := runtime.Register("hello.greet", greetingHandler{messages: messages}); err != nil {
		log.Fatal(err)
	}
	if _, err := runtime.Add("hello.greet", greeting{Name: "Cellar"}); err != nil {
		log.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()

	fmt.Println(<-messages)
	if err := runtime.Stop(); err != nil {
		log.Fatal(err)
	}
	if err := <-done; err != nil {
		log.Fatal(err)
	}
}
