package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/packets"
	"github.com/eclipse/paho.golang/paho"
	"go.uber.org/zap"
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
	logger  *zap.Logger
	cancel  context.CancelFunc

	terminalOnce   sync.Once
	terminalErrors chan error

	mu            sync.RWMutex
	subscriptions []subscription
}

func Connect(ctx context.Context, cfg Config, logger *zap.Logger) (*Client, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	serverURL, tlsConfig, err := connectionConfig(cfg)
	if err != nil {
		return nil, err
	}

	managerContext, cancelManager := context.WithCancel(ctx)
	client := &Client{
		logger:         logger,
		cancel:         cancelManager,
		terminalErrors: make(chan error, 1),
	}
	logger.Debug("mqtt connecting",
		zap.String("client_id", cfg.ClientID),
		zap.String("broker", serverURL.Host),
		zap.Bool("tls", cfg.TLS.Enabled),
	)
	manager, err := autopaho.NewConnection(managerContext, autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{serverURL},
		TlsCfg:                        tlsConfig,
		KeepAlive:                     30,
		CleanStartOnInitialConnection: true,
		SessionExpiryInterval:         0,
		ConnectTimeout:                10 * time.Second,
		ReconnectBackoff:              reconnectBackoff(),
		ConnectUsername:               cfg.Username,
		ConnectPassword:               []byte(cfg.Password),
		OnConnectionUp: func(manager *autopaho.ConnectionManager, connack *paho.Connack) {
			logger.Debug("mqtt connection established",
				zap.String("client_id", cfg.ClientID),
				zap.String("broker", serverURL.Host),
				zap.Bool("session_present", connack.SessionPresent),
			)
			client.resubscribe(manager)
		},
		OnConnectError: func(err error) {
			logger.Warn("mqtt connection failed", zap.Error(err))
		},
		ClientConfig: paho.ClientConfig{
			ClientID: cfg.ClientID,
			OnServerDisconnect: func(disconnect *paho.Disconnect) {
				if err := terminalDisconnectError(cfg.ClientID, disconnect); err != nil {
					client.terminate(err)
				}
			},
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				client.route,
			},
		},
	})
	if err != nil {
		cancelManager()
		return nil, fmt.Errorf("create mqtt connection: %w", err)
	}
	client.manager = manager
	if err := manager.AwaitConnection(ctx); err != nil {
		cancelManager()
		return nil, fmt.Errorf("connect mqtt: %w", err)
	}
	return client, nil
}

func reconnectBackoff() autopaho.Backoff {
	return autopaho.NewExponentialBackoff(time.Second, 30*time.Second, 2*time.Second, 2)
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
	c.logger.Debug("mqtt message published",
		zap.String("topic", message.Topic),
		zap.Int("qos", 1),
		zap.Bool("retained", message.Retain),
		zap.Int("bytes", len(message.Payload)),
	)
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
	if err := c.manager.Disconnect(ctx); err != nil {
		return err
	}
	c.logger.Debug("mqtt disconnected")
	return nil
}

// TerminalErrors reports connection failures that must not be retried.
func (c *Client) TerminalErrors() <-chan error {
	return c.terminalErrors
}

func (c *Client) terminate(err error) {
	c.terminalOnce.Do(func() {
		c.terminalErrors <- err
		c.cancel()
	})
}

func terminalDisconnectError(clientID string, disconnect *paho.Disconnect) error {
	if disconnect == nil || disconnect.ReasonCode != packets.DisconnectSessionTakenOver {
		return nil
	}
	return fmt.Errorf("mqtt identity conflict: another connection is using client_id %q (session taken over)", clientID)
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
	c.logger.Debug("mqtt subscription active", zap.String("topic", filter), zap.Int("qos", 1))
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
				c.logger.Warn("mqtt resubscribe failed", zap.String("topic", filter), zap.Error(err))
				return
			}
			accepted := true
			for _, reason := range response.Reasons {
				if reason >= 0x80 {
					accepted = false
					c.logger.Warn("mqtt resubscribe rejected", zap.String("topic", filter), zap.Int("reason", int(reason)))
				}
			}
			if accepted {
				c.logger.Debug("mqtt subscription restored", zap.String("topic", filter), zap.Int("qos", 1))
			}
		}()
	}
}

func (c *Client) route(message paho.PublishReceived) (bool, error) {
	c.logger.Debug("mqtt message received",
		zap.String("topic", message.Packet.Topic),
		zap.Int("qos", int(message.Packet.QoS)),
		zap.Bool("retained", message.Packet.Retain),
		zap.Int("bytes", len(message.Packet.Payload)),
	)
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
			c.logger.Warn("mqtt message rejected", zap.String("topic", message.Packet.Topic), zap.Error(err))
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
