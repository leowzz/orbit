package core

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"orbit/internal/config"
	"orbit/internal/mqttadmin"
)

// These routes share the console's session and same-origin checks.
func mqttAdminRoutes(mux *http.ServeMux, cfg *config.CoreConfig) {
	client := mqttadmin.New(cfg.MQTTAdmin.URL, cfg.MQTTAdmin.APIKey, cfg.MQTTAdmin.APISecret, cfg.MQTT.Credentials.Username)
	var operations sync.Mutex
	fail := func(w http.ResponseWriter, err error) {
		e := mqttadmin.PublicError(err)
		http.Error(w, e.Message, e.Status)
	}
	mux.HandleFunc("GET /api/mqtt/users", func(w http.ResponseWriter, r *http.Request) {
		if !cfg.MQTTAdmin.Enabled() {
			writeConsoleJSON(w, map[string]any{"enabled": false, "users": []mqttadmin.User{}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		users, err := client.List(ctx)
		if err != nil {
			fail(w, err)
			return
		}
		writeConsoleJSON(w, map[string]any{"enabled": true, "users": users, "broker_url": cfg.MQTT.URL})
	})
	mutate := func(action string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !cfg.MQTTAdmin.Enabled() {
				http.Error(w, "请先在 Core 配置文件中填写 mqtt_admin 的地址、API Key 和 Secret，并重启 Core", 503)
				return
			}
			if !operations.TryLock() {
				http.Error(w, "正在处理其他 MQTT 账号操作，请稍后重试", 409)
				return
			}
			defer operations.Unlock()
			var input mqttadmin.CreateInput
			if action == "create" {
				media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if media != "application/json" {
					http.Error(w, "expected application/json", 415)
					return
				}
				decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
				decoder.DisallowUnknownFields()
				if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
					http.Error(w, "MQTT 账号信息格式无效", 400)
					return
				}
			}
			// Finish multi-step broker changes even if the browser closes mid-request.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
			defer cancel()
			var result mqttadmin.Result
			var err error
			switch action {
			case "create":
				result, err = client.Create(ctx, input)
			case "rotate":
				result, err = client.Rotate(ctx, r.PathValue("username"))
			case "revoke":
				result, err = client.Revoke(ctx, r.PathValue("username"))
			case "disconnect":
				result, err = client.Disconnect(ctx, r.PathValue("username"))
			}
			if err != nil {
				fail(w, err)
				return
			}
			writeConsoleJSON(w, result)
		}
	}
	mux.HandleFunc("POST /api/mqtt/users", mutate("create"))
	mux.HandleFunc("POST /api/mqtt/users/{username}/rotate", mutate("rotate"))
	mux.HandleFunc("POST /api/mqtt/users/{username}/revoke", mutate("revoke"))
	mux.HandleFunc("POST /api/mqtt/users/{username}/disconnect", mutate("disconnect"))
}
