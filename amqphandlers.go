package main

import (
	"encoding/json"
	"sync"

	"github.com/streadway/amqp"
)

func HandleNotificationDeliveries(deliveries <-chan amqp.Delivery, done *sync.WaitGroup) {
	for d := range deliveries {
		Log(
			"got %dB delivery: [%v]",
			len(d.Body),
			d.DeliveryTag,
		)

		// Handle delivery
		n := Notification{}
		if err := json.Unmarshal(d.Body, &n); err != nil {
			Log("[%q] Couldn't deserialize notification. Sending to dead-letter exchange with 'reject': %v", d.DeliveryTag, err)
			if err := d.Reject(
				false, // requeue
			); err != nil {
				LogErrFormat("[%q] Rejecting message: %v", d.DeliveryTag, err)
			}
			continue
		}
		if err := n.Validate(); err != nil {
			Log("[%q] Invalid notification received. Sending to dead-letter exchange with 'reject': %v", d.DeliveryTag, err)
			if err := d.Reject(
				false, // requeue
			); err != nil {
				LogErrFormat("[%q] Rejecting message: %v", d.DeliveryTag, err)
			}
			continue
		}
		if err := ProcessNotification(n); err != nil {
			LogErrFormat("[%q] Processing notification failed. Requeuing with 'nack': %v", d.DeliveryTag, err)
			if err := d.Nack(
				false, // multiple
				true,  // requeue
			); err != nil {
				LogErrFormat("[%q] Nacking message for redelivery: %v", d.DeliveryTag, err)
			}
			continue
		}
		Log("Notification processed. Acking.")

		d.Ack(false)
	}
	Log("handle: deliveries channel closed")
	done.Done()
}
