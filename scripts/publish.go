package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/dhananjayksharma/tenant-control-plane/internal/messaging"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	taskID := flag.String("task-id", "", "task UUID")
	eventID := flag.String("event-id", "manual-1", "stable event id")
	status := flag.String("status", "in_progress", "status")
	raw := flag.String("raw", "", "raw message to inject")
	flag.Parse()
	url := os.Getenv("AMQP_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}
	c, e := amqp.Dial(url)
	if e != nil {
		log.Fatal(e)
	}
	defer c.Close()
	ch, e := c.Channel()
	if e != nil {
		log.Fatal(e)
	}
	defer ch.Close()
	if e = messaging.Declare(ch); e != nil {
		log.Fatal(e)
	}
	body := []byte(*raw)
	if *raw == "" {
		if *taskID == "" {
			log.Fatal("--task-id required")
		}
		body, e = json.Marshal(messaging.ProgressEvent{EventID: *eventID, TaskID: *taskID, Status: *status})
		if e != nil {
			log.Fatal(e)
		}
	}
	if e = messaging.Publish(ch, messaging.ProgressQueue, body); e != nil {
		log.Fatal(e)
	}
	log.Printf("published %s", body)
}
