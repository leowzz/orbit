package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	orbitv1 "orbit/gen/go/orbit/v1"
	webnode "orbit/nodes/web"
)

func TestHTTPServerShutdownCancelsSSE(t *testing.T) {
	serverContext, cancelServer := context.WithCancel(context.Background())
	store := webnode.NewStore()
	if err := store.Update(&orbitv1.DeviceView{
		Metadata:  &orbitv1.Metadata{Revision: 1},
		NodeId:    "web-test",
		CoreEpoch: "core-test",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer(serverContext, webnode.Handler(store, nil))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancelServer()
		_ = server.Close()
	})
	go func() { _ = server.Serve(listener) }()

	response, err := http.Get("http://" + listener.Addr().String() + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	cancelServer()
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("shutdown SSE request: %v", err)
	}
}
