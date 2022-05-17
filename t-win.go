package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"
)

func main() {
	pid := os.Getpid()
	fmt.Printf("pid: %v\n", pid)

	c := make(chan os.Signal, 10)
	signal.Notify(c)
	select {
	case s := <-c:
		if s != os.Interrupt {
			log.Fatalf("Wrong signal received: got %q, want %q\n", s, os.Interrupt)
		}
	case <-time.After(60 * time.Second):
		log.Fatalf("Timeout waiting for Ctrl+Break\n")
	}
}
