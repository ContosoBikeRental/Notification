package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/streadway/amqp"
)

type Notification struct {
	RecipientUserID string
	Subject         string
	Message         string
}

func (n Notification) Serialize() (string, error) {
	nBytes, err := json.Marshal(n)
	if err != nil {
		return "", AddMyInfoToErr(err)
	}

	return string(nBytes), nil
}

func (n Notification) Validate() error {
	var errorSlice []string
	var zeroString string

	if n.Message == zeroString && n.Subject == zeroString {
		errorSlice = append(errorSlice, "Must provide a non-empty Subject or Message")
	}

	// TODO validate that Recipient is a valid userID

	if len(errorSlice) > 0 {
		errorBytes, err := json.Marshal(errorSlice)
		if err != nil {
			return AddMyInfoToErr(err)
		}

		return errors.New(string(errorBytes))
	}

	return nil
}

func ProcessNotification(context *RequestContext, n Notification) error {
	if err := n.Validate(); err != nil {
		return AddMyInfoToErr(err)
	}

	LogWithContext(context, "Processing notification")

	var serializedNotification string
	var err error
	if serializedNotification, err = n.Serialize(); err != nil {
		return AddMyInfoToErr(err)
	}

	LogWithContext(context, "Notification is: %s", serializedNotification)

	if n.RecipientUserID != "" {
		LogWithContext(context, "Notifying user (%s)", n.RecipientUserID)
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
				Body:        []byte(serializedNotification),
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
