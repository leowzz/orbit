package appapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"github.com/google/uuid"
	"net"
	"net/http"
	"net/url"
	"time"

	"orbit/internal/config"
	"orbit/internal/inbox"
	"orbit/internal/mqttadmin"
)

type deviceMQTTProvisioner interface {
	ProvisionNode(context.Context, string, string, string) error
	Revoke(context.Context, string) (mqttadmin.Result, error)
}

// ConfigureMQTT runs before serving requests. Passwords are derived, never stored
// in the device registry or exposed through device lists/status/QR codes.
func (s *Server) ConfigureMQTT(cfg *config.CoreConfig) {
	if !cfg.MQTTAdmin.Enabled() {
		return
	}
	address := cfg.MQTTAdmin.NodeURL
	if address == "" {
		address = cfg.MQTT.URL
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return
	}
	if u.Port() == "" {
		port := "1883"
		if u.Scheme == "mqtts" {
			port = "8883"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	s.mqttURL, s.mqttSecret = u.String(), cfg.MQTTAdmin.APISecret
	s.mqttProvisioner = mqttadmin.New(cfg.MQTTAdmin.URL, cfg.MQTTAdmin.APIKey, cfg.MQTTAdmin.APISecret, cfg.MQTT.Credentials.Username)
}

func (s *Server) provisionMQTT(w http.ResponseWriter, r *http.Request) {
	// Serialize provisioning with token rotation/revocation/deletion, and check
	// authentication after acquiring the lock so an old token cannot claim anew.
	s.mqttMu.Lock()
	defer s.mqttMu.Unlock()
	s.auth(func(w http.ResponseWriter, r *http.Request, node string) {
		s.mu.Lock()
		device := s.devices[node]
		s.mu.Unlock()
		if device.Platform != "android" && device.Platform != "windows" && device.Platform != "" {
			failure(w, &inbox.Fault{Code: "forbidden"})
			return
		}
		if s.mqttProvisioner == nil {
			write(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}
		if device.MQTTUsername == "" {
			// Reserve ownership before any broker mutation, including a lost reply.
			s.mu.Lock()
			device = s.devices[node]
			device.MQTTUsername = "orbit-app-" + uuid.NewString()
			err := s.saveDevice(r, node, device)
			s.mu.Unlock()
			if err != nil {
				failure(w, err)
				return
			}
		}
		mac := hmac.New(sha256.New, []byte(s.mqttSecret))
		mac.Write([]byte("orbit-app-mqtt\x00" + node + "\x00" + device.TokenSHA256))
		password := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.mqttProvisioner.ProvisionNode(ctx, device.MQTTUsername, node, password); err != nil {
			mqttFailure(w)
			return
		}
		write(w, http.StatusOK, map[string]any{"enabled": true, "mqtt": map[string]string{
			"uri": s.mqttURL, "nodeId": node, "username": device.MQTTUsername, "password": password,
		}})
	})(w, r)
}

func mqttFailure(w http.ResponseWriter) {
	write(w, http.StatusServiceUnavailable, map[string]string{"code": "mqtt_unavailable", "message": "MQTT 账号处理未完成，请检查管理连接后重试"})
}

// The caller holds mqttMu. Failure leaves the App authorization unchanged so
// the operator can retry without silently leaving a live broker credential.
func (s *Server) revokeDeviceMQTT(w http.ResponseWriter, r *http.Request, id string) bool {
	s.mu.Lock()
	username := s.devices[id].MQTTUsername
	s.mu.Unlock()
	if username == "" {
		return true
	}
	if s.mqttProvisioner == nil {
		mqttFailure(w)
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := s.mqttProvisioner.Revoke(ctx, username)
	if err != nil || result.Warning != "" {
		mqttFailure(w)
		return false
	}
	return true
}
