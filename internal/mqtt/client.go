package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
)

const contentTypeProtobuf = "application/protobuf"

type TLSConfig struct {
	Enabled  bool
	CAFile   string
	CertFile string
	KeyFile  string
}

type Config struct {
	URL      string
	ClientID string
	Username string
	Password string
	TLS      TLSConfig
}

type Message struct {
	Topic   string
	Payload []byte
	Retain  bool
}

type Handler func(context.Context, Message) error

type subscription struct {
	filter  string
	handler Handler
}

// Client owns an MQTT 5 connection and restores registered subscriptions after reconnects.
type Client struct {
	manager *autopaho.ConnectionManager
	logger  *slog.Logger

	mu            sync.RWMutex
	subscriptions []subscription
}

func Connect(ctx context.Context, cfg Config, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	serverURL, tlsConfig, err := connectionConfig(cfg)
	if err != nil {
		return nil, err
	}

	client := &Client{logger: logger}
	manager, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{serverURL},
		TlsCfg:                        tlsConfig,
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		SessionExpiryInterval:         0,
		ConnectTimeout:                10 * time.Second,
		ReconnectBackoff:              autopaho.NewExponentialBackoff(time.Second, 30*time.Second, 2*time.Second, 0.2),
		ConnectUsername:               cfg.Username,
		ConnectPassword:               []byte(cfg.Password),
		OnConnectionUp: func(manager *autopaho.ConnectionManager, _ *paho.Connack) {
			client.resubscribe(manager)
		},
		OnConnectError: func(err error) {
			logger.Warn("mqtt connection failed", "error", err)
		},
		ClientConfig: paho.ClientConfig{
			ClientID: cfg.ClientID,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				client.route,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create mqtt connection: %w", err)
	}
	client.manager = manager
	if err := manager.AwaitConnection(ctx); err != nil {
		return nil, fmt.Errorf("connect mqtt: %w", err)
	}
	return client, nil
}

func (c *Client) Publish(ctx context.Context, message Message) error {
	response, err := c.manager.Publish(ctx, &paho.Publish{
		QoS:        1,
		Retain:     message.Retain,
		Topic:      message.Topic,
		Payload:    message.Payload,
		Properties: &paho.PublishProperties{ContentType: contentTypeProtobuf},
	})
	if err != nil {
		return fmt.Errorf("publish %q: %w", message.Topic, err)
	}
	if response != nil && response.ReasonCode >= 0x80 {
		return fmt.Errorf("publish %q rejected with reason 0x%x", message.Topic, response.ReasonCode)
	}
	return nil
}

func (c *Client) Subscribe(ctx context.Context, filter string, handler Handler) error {
	if filter == "" || handler == nil {
		return errors.New("mqtt subscription requires a filter and handler")
	}
	c.mu.Lock()
	c.subscriptions = append(c.subscriptions, subscription{filter: filter, handler: handler})
	c.mu.Unlock()
	if err := c.subscribe(ctx, filter); err != nil {
		c.mu.Lock()
		c.subscriptions = c.subscriptions[:len(c.subscriptions)-1]
		c.mu.Unlock()
		return err
	}
	return nil
}

func (c *Client) Disconnect(ctx context.Context) error {
	return c.manager.Disconnect(ctx)
}

func (c *Client) subscribe(ctx context.Context, filter string) error {
	response, err := c.manager.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{
		Topic:          filter,
		QoS:            1,
		RetainHandling: 0,
	}}})
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", filter, err)
	}
	for _, reason := range response.Reasons {
		if reason >= 0x80 {
			return fmt.Errorf("subscribe %q rejected with reason 0x%x", filter, reason)
		}
	}
	return nil
}

func (c *Client) resubscribe(manager *autopaho.ConnectionManager) {
	c.mu.RLock()
	filters := make([]string, 0, len(c.subscriptions))
	for _, item := range c.subscriptions {
		filters = append(filters, item.filter)
	}
	c.mu.RUnlock()
	for _, filter := range filters {
		filter := filter
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response, err := manager.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: filter, QoS: 1}}})
			if err != nil {
				c.logger.Warn("mqtt resubscribe failed", "topic", filter, "error", err)
				return
			}
			for _, reason := range response.Reasons {
				if reason >= 0x80 {
					c.logger.Warn("mqtt resubscribe rejected", "topic", filter, "reason", reason)
				}
			}
		}()
	}
}

func (c *Client) route(message paho.PublishReceived) (bool, error) {
	c.mu.RLock()
	subscriptions := append([]subscription(nil), c.subscriptions...)
	c.mu.RUnlock()

	handled := false
	for _, item := range subscriptions {
		if !topicMatches(item.filter, message.Packet.Topic) {
			continue
		}
		handled = true
		payload := append([]byte(nil), message.Packet.Payload...)
		if err := item.handler(context.Background(), Message{
			Topic:   message.Packet.Topic,
			Payload: payload,
			Retain:  message.Packet.Retain,
		}); err != nil {
			c.logger.Warn("mqtt message rejected", "topic", message.Packet.Topic, "error", err)
		}
	}
	return handled, nil
}

func connectionConfig(cfg Config) (*url.URL, *tls.Config, error) {
	if cfg.ClientID == "" {
		return nil, nil, errors.New("mqtt client id is required")
	}
	serverURL, err := url.Parse(cfg.URL)
	if err != nil || serverURL.Host == "" {
		return nil, nil, fmt.Errorf("invalid mqtt url %q", cfg.URL)
	}

	if !cfg.TLS.Enabled {
		if serverURL.Scheme != "mqtt" {
			return nil, nil, errors.New("mqtt TLS is disabled but URL scheme is not mqtt")
		}
		return serverURL, nil, nil
	}
	if serverURL.Scheme != "mqtts" && serverURL.Scheme != "tls" {
		return nil, nil, errors.New("mqtt TLS is enabled but URL scheme is not mqtts or tls")
	}
	serverURL.Scheme = "tls"
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverURL.Hostname()}
	if cfg.TLS.CAFile != "" {
		pem, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, nil, fmt.Errorf("read mqtt CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, nil, errors.New("mqtt CA file contains no certificates")
		}
		tlsConfig.RootCAs = pool
	}
	if (cfg.TLS.CertFile == "") != (cfg.TLS.KeyFile == "") {
		return nil, nil, errors.New("mqtt client certificate and key must be configured together")
	}
	if cfg.TLS.CertFile != "" {
		certificate, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("load mqtt client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return serverURL, tlsConfig, nil
}

func topicMatches(filter, topic string) bool {
	filterParts := strings.Split(filter, "/")
	topicParts := strings.Split(topic, "/")
	for index, part := range filterParts {
		if part == "#" {
			return index == len(filterParts)-1
		}
		if index >= len(topicParts) {
			return false
		}
		if part != "+" && part != topicParts[index] {
			return false
		}
	}
	return len(filterParts) == len(topicParts)
}
