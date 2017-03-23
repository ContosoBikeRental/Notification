package main

import (
	"encoding/json"
	"sync"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/streadway/amqp"
)

const (
	RequestIDHeaderName = "x-contoso-request-id"
)

type RequestContext struct {
	RequestID *uuid.UUID
}

func HandleNotificationDeliveries(deliveries <-chan amqp.Delivery, done *sync.WaitGroup) {
	for d := range deliveries {
		Log(
			"got %dB delivery: [%v]",
			len(d.Body),
			d.DeliveryTag,
		)

		// Get request context
		headerVal := d.Headers[RequestIDHeaderName]
		reqIDStr, ok := headerVal.(string)
		if !ok {
			LogErrFormat("Unexpected type for request ID. Got: '%v'. Setting to empty GUID string.", headerVal)
			reqIDStr = (&uuid.UUID{}).String()
		}

		reqUUID, err := uuid.ParseHex(reqIDStr)
		if err != nil {
			LogErrFormat("Couldn't parse request id. Got: '%s'. Setting to empty GUID.", reqIDStr)
			reqUUID = &uuid.UUID{}
		}

		requestContext := &RequestContext{reqUUID}

		// Handle delivery
		n := Notification{}
		if err := json.Unmarshal(d.Body, &n); err != nil {
			LogWithContext(requestContext, "[%q] Couldn't deserialize notification. Sending to dead-letter exchange with 'reject': %v", d.DeliveryTag, err)
			if err := d.Reject(
				false, // requeue
			); err != nil {
				LogErrFormatWithContext(requestContext, "[%q] Rejecting message: %v", d.DeliveryTag, err)
			}
			continue
		}
		if err := n.Validate(); err != nil {
			LogWithContext(requestContext, "[%q] Invalid notification received. Sending to dead-letter exchange with 'reject': %v", d.DeliveryTag, err)
			if err := d.Reject(
				false, // requeue
			); err != nil {
				LogErrFormatWithContext(requestContext, "[%q] Rejecting message: %v", d.DeliveryTag, err)
			}
			continue
		}
		if err := ProcessNotification(requestContext, n); err != nil {
			LogErrFormatWithContext(requestContext, "[%q] Processing notification failed. Requeuing with 'nack': %v", d.DeliveryTag, err)
			if err := d.Nack(
				false, // multiple
				true,  // requeue
			); err != nil {
				LogErrFormatWithContext(requestContext, "[%q] Nacking message for redelivery: %v", d.DeliveryTag, err)
			}
			continue
		}
		LogWithContext(requestContext, "Notification processed. Acking.")

		if err := d.Ack(false); err != nil {
			LogErrFormatWithContext(requestContext, "[%q] Error ACKing delivery: %v", err)
		}
	}
	Log("handle: deliveries channel closed")
	done.Done()
}
