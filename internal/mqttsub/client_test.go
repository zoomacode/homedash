package mqttsub

import (
	"context"
	"net"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttserver "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/zoomacode/homedash/internal/config"
	"github.com/zoomacode/homedash/internal/state"
)

func startBroker(t *testing.T) (string, func()) {
	t.Helper()
	srv := mqttserver.New(nil)
	_ = srv.AddHook(new(authAllow), nil)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	if err := srv.AddListener(listeners.NewTCP(listeners.Config{ID: "t1", Address: addr})); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	time.Sleep(100 * time.Millisecond)
	return addr, func() { srv.Close() }
}

type authAllow struct{ mqttserver.HookBase }

func (h *authAllow) ID() string { return "allow" }
func (h *authAllow) Provides(b byte) bool {
	return b == mqttserver.OnConnectAuthenticate || b == mqttserver.OnACLCheck
}
func (h *authAllow) OnConnectAuthenticate(_ *mqttserver.Client, _ packets.Packet) bool { return true }
func (h *authAllow) OnACLCheck(_ *mqttserver.Client, _ string, _ bool) bool            { return true }

func TestClient_StoresIncomingValues(t *testing.T) {
	addr, stop := startBroker(t)
	defer stop()

	st := state.New()
	c := New(Config{
		Broker:   "tcp://" + addr,
		ClientID: "test-sub",
		Topics: []config.Topic{{
			Topic: "sensors/temp", Name: "Temp", Unit: "°C", Group: "outdoor",
			Decimals: 1, StaleAfter: 5 * time.Minute,
		}},
	}, st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}

	pubOpts := mqtt.NewClientOptions().AddBroker("tcp://" + addr).SetClientID("pub")
	pub := mqtt.NewClient(pubOpts)
	if tok := pub.Connect(); tok.Wait() && tok.Error() != nil {
		t.Fatal(tok.Error())
	}
	tok := pub.Publish("sensors/temp", 0, false, "21.5")
	tok.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s := st.Snapshot().Sensors["sensors/temp"]
		if s.Value == "21.5" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("sensor not stored, got %#v", st.Snapshot().Sensors)
}
