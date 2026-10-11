// SPDX-License-Identifier: AGPL-3.0-only
package brig

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type brigMailSender interface {
	send(context.Context, brigMail) error
}
type registrationMailDelivery struct {
	renderer registrationMailRenderer
	sender   brigMailSender
	feedback *userJournal
	shutdown func()
}

func (*registrationMailDelivery) String() string { return "<RegistrationMailDelivery>" }

func newRegistrationMailDelivery(ctx context.Context, c Config, warn func(string)) (*registrationMailDelivery, error) {
	renderer, err := newRegistrationMailRenderer(c, warn)
	if err != nil {
		return nil, err
	}
	options, ok := c.Other["emailSMS"].(map[string]any)
	if !ok {
		return nil, errors.New("mail configuration unavailable")
	}
	raw := options["email"]
	if ses, err := parseSESMailConfig(raw); err == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		// Brig.App native AWS manager uses10s response timeout and1024 connections.
		transport.ResponseHeaderTimeout = 10 * time.Second
		transport.MaxConnsPerHost = 1024
		transport.MaxIdleConns = 4096
		transport.MaxIdleConnsPerHost = 1024
		client := &http.Client{Transport: transport}
		sender, err := newSESMailSender(ctx, ses, client)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, err
		}
		// Native startup resolves the SES feedback queue before accepting requests.
		feedback, err := newBrigSQS(ctx, c.AWS, ses.feedbackQueue, client)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, err
		}
		return &registrationMailDelivery{renderer: renderer, sender: sender, feedback: feedback, shutdown: transport.CloseIdleConnections}, nil
	}
	smtp, err := parseSMTPMailConfig(raw)
	if err != nil {
		return nil, errors.New("mail transport configuration invalid")
	}
	sender, err := newSMTPMailSender(ctx, smtp)
	if err != nil {
		return nil, err
	}
	return &registrationMailDelivery{renderer: renderer, sender: sender, shutdown: sender.close}, nil
}
func (d *registrationMailDelivery) activation(ctx context.Context, created registrationCreation, origin registrationOrigin) error {
	if d == nil || d.sender == nil {
		return errors.New("mail delivery unavailable")
	}
	message, err := d.renderer.activation(created, origin)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}
func (d *registrationMailDelivery) welcome(ctx context.Context, profile selfProfile, team registrationCreatedTeam) error {
	if d == nil || d.sender == nil {
		return errors.New("mail delivery unavailable")
	}
	message, err := d.renderer.welcome(profile, team)
	if err != nil {
		return err
	}
	return d.sender.send(ctx, message)
}
func (d *registrationMailDelivery) close() {
	if d != nil && d.shutdown != nil {
		d.shutdown()
	}
}
