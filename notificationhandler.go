package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/streadway/amqp"
)

type ReservationDetails struct {
	ReservationID string `bson:"reservationId" json:"reservationId"`
	BikeID        string `bson:"bikeId" json:"bikeId"`
	UserID        string `bson:"userId" json:"userId"`
	RequestTime   string `bson:"requestTime" json:"requestTime"`
	StartTime     string `bson:"startTime" json:"startTime"`
	EndTime       string `bson:"endTime" json:"endTime"`
	State         string `bson:"state" json:"state"`
	InvoiceID     string `bson:"invoiceId" json:"invoiceId"`
}

func (r *ReservationDetails) Serialize() (string, error) {
	rBytes, err := json.Marshal(r)
	if err != nil {
		return "", AddMyInfoToErr(err)
	}

	return string(rBytes), nil
}

func ProcessNotification(context *RequestContext, r *ReservationDetails) error {
	LogWithContext(context, "Processing notification")

	var serializedReservation string
	var err error
	if serializedReservation, err = r.Serialize(); err != nil {
		return AddMyInfoToErr(err)
	}

	if r.UserID != "" {
		LogWithContext(context, "Notifying user (%s) of reservation (%s) with state (%s)", r.UserID, r.ReservationID, r.State)
		/*
		 * Send email/text to user
		 */
		LogWithContext(context, "Sending email... but not really")
		LogWithContext(context, "Sending Twilio notification... but not really")
		LogWithContext(context, "User notified")
	}

	LogWithContext(context, "Publishing notification to outbound queue")

	// Try to publish completion
	maxTries := 3
	complete := false
	for i := 1; i <= maxTries; i++ {
		if err = PubAmqpConnection.Publish(
			EnvPubExchange,
			context.RequestID.String(),
			amqp.Publishing{
				ContentType: "application/json",
				Headers: amqp.Table{
					RequestIDHeaderName: context.RequestID.String(),
				},
				Body: []byte(serializedReservation),
			},
		); err != nil {
			LogErrFormatWithContext(context, "%d/%d Publish failed: %v", i, maxTries, err)
			time.Sleep(1 * time.Second)
			continue
		}

		complete = true
		break
	}

	if !complete {
		return fmt.Errorf("Failed to publish notification to outbound queue! (%s)", context.RequestID.String())
	}

	LogWithContext(context, "Notification complete")
	return nil
}
