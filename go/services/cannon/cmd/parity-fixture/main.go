// Synthetic private parity fixture only; this utility is not packaged in cannon.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	cannon "github.com/adamjlow/wire-server/go/services/cannon"
	"github.com/apache/cassandra-gocql-driver/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	file := flag.String("config", "", "private synthetic config")
	mode := flag.String("mode", "", "set-missed/read-missed/delete-missed/publish/delete-queue")
	sequence := flag.Int("sequence", 0, "synthetic event sequence")
	flag.Parse()
	if os.Getenv("WIRE_E2E_DISPOSABLE") != "1" {
		fmt.Fprintln(os.Stderr, "private fixture required")
		os.Exit(2)
	}
	c, err := cannon.LoadConfig(*file)
	if err != nil {
		fail(err)
	}
	b, err := cannon.Connect(c)
	if err != nil {
		fail(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	user, err := gocql.ParseUUID("33333333-3333-4333-8333-333333333333")
	if err != nil {
		fail(err)
	}
	client := "abcd"
	queue := "user-notifications." + user.String() + "." + client
	result := map[string]any{"mode": *mode}
	switch *mode {
	case "set-missed":
		err = b.Cassandra.Query("INSERT INTO missed_notifications(user_id, client_id) VALUES (?, ?)", user, client).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec()
	case "read-missed":
		var id gocql.UUID
		err = b.Cassandra.Query("SELECT user_id FROM missed_notifications WHERE user_id = ? AND client_id = ?", user, client).WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&id)
		result["missed"] = err == nil
		if err == gocql.ErrNotFound {
			err = nil
		}
	case "delete-missed":
		err = b.Cassandra.Query("DELETE FROM missed_notifications WHERE user_id = ? AND client_id = ?", user, client).WithContext(ctx).Consistency(gocql.LocalQuorum).Exec()
	case "publish", "delete-queue":
		channel, channelErr := b.Rabbit.Channel()
		if channelErr != nil {
			fail(channelErr)
		}
		defer channel.Close()
		if *mode == "delete-queue" {
			_, err = channel.QueueDelete(queue, false, false, false)
		} else {
			body, _ := json.Marshal(map[string]any{"id": "44444444-4444-4444-8444-444444444444", "payload": []any{map[string]any{"type": "synthetic-switchback", "sequence": *sequence}}})
			if err = channel.Confirm(false); err != nil {
				fail(err)
			}
			confirmed := channel.NotifyPublish(make(chan amqp.Confirmation, 1))
			err = channel.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body})
			if err == nil {
				select {
				case ack := <-confirmed:
					if !ack.Ack {
						err = fmt.Errorf("publication rejected")
					}
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
		}
	default:
		fail(fmt.Errorf("unknown fixture action"))
	}
	if err != nil {
		fail(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
func fail(error) { fmt.Fprintln(os.Stderr, "private fixture action failed"); os.Exit(1) }
