package clock

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/beevik/ntp"
	"go.uber.org/zap"
)

type Config struct {
	Server       string
	SyncInterval time.Duration
	Timeout      time.Duration
}

type measurement struct {
	offset time.Duration
	rtt    time.Duration
}

type Clock struct {
	config    Config
	logger    *zap.Logger
	systemNow func() time.Time
	query     func(string, time.Duration) (measurement, error)

	mu     sync.RWMutex
	offset time.Duration
}

func New(config Config, logger *zap.Logger) (*Clock, error) {
	if strings.TrimSpace(config.Server) == "" {
		return nil, errors.New("NTP server is required")
	}
	if config.SyncInterval <= 0 || config.Timeout <= 0 {
		return nil, errors.New("NTP sync interval and timeout must be positive")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Clock{
		config:    config,
		logger:    logger,
		systemNow: time.Now,
		query:     queryNTP,
	}, nil
}

func (c *Clock) Start(ctx context.Context) {
	c.sync()
	go func() {
		ticker := time.NewTicker(c.config.SyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sync()
			}
		}
	}()
}

func (c *Clock) Now() time.Time {
	c.mu.RLock()
	offset := c.offset
	c.mu.RUnlock()
	return c.systemNow().Add(offset)
}

func (c *Clock) sync() {
	measurement, err := c.query(c.config.Server, c.config.Timeout)
	if err != nil {
		c.logger.Warn("ntp synchronization failed",
			zap.String("server", c.config.Server),
			zap.Error(err),
		)
		return
	}
	c.mu.Lock()
	c.offset = measurement.offset
	c.mu.Unlock()
	c.logger.Info("ntp synchronized",
		zap.String("server", c.config.Server),
		zap.Int64("offset_ms", measurement.offset.Milliseconds()),
		zap.Int64("rtt_ms", measurement.rtt.Milliseconds()),
	)
}

func queryNTP(server string, timeout time.Duration) (measurement, error) {
	response, err := ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout})
	if err != nil {
		return measurement{}, err
	}
	if err := response.Validate(); err != nil {
		return measurement{}, err
	}
	return measurement{offset: response.ClockOffset, rtt: response.RTT}, nil
}
