// Package mqttsub subscribes to configured MQTT topics and stores readings into state.
package mqttsub

import (
	"context"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/zoomacode/homedash/internal/config"
	"github.com/zoomacode/homedash/internal/state"
)

type Config struct {
	Broker   string
	ClientID string
	Topics   []config.Topic
}

type Client struct {
	cfg   Config
	store *state.Store
	mc    mqtt.Client
}

func New(cfg Config, store *state.Store) *Client {
	return &Client{cfg: cfg, store: store}
}

func (c *Client) Start(ctx context.Context) error {
	opts := mqtt.NewClientOptions().
		AddBroker(c.cfg.Broker).
		SetClientID(c.cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetMaxReconnectInterval(60 * time.Second).
		SetOnConnectHandler(c.onConnect)
	c.mc = mqtt.NewClient(opts)
	tok := c.mc.Connect()
	go func() {
		<-ctx.Done()
		c.mc.Disconnect(250)
	}()
	tok.Wait()
	return tok.Error()
}

func (c *Client) onConnect(mc mqtt.Client) {
	for _, t := range c.cfg.Topics {
		topic := t // capture
		mc.Subscribe(topic.Topic, 0, func(_ mqtt.Client, m mqtt.Message) {
			val, err := decodeValue(m.Payload())
			if err != nil {
				log.Printf("mqtt decode %s: %v", m.Topic(), err)
				return
			}
			c.store.SetSensor(state.Sensor{
				Topic: topic.Topic, Name: topic.Name, Unit: topic.Unit,
				Group: topic.Group, Value: val, StaleAfter: topic.StaleAfter,
			})
		})
	}
}
