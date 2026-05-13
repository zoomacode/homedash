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
	Username string
	Password string
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
	if c.cfg.Username != "" {
		opts.SetUsername(c.cfg.Username)
	}
	if c.cfg.Password != "" {
		opts.SetPassword(c.cfg.Password)
	}
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
	// Group entries by MQTT topic so a single payload can fan out into
	// multiple sensors via different field selectors. We also remember
	// each entry's position in the config so the dashboard can render
	// sensors in the order the user listed them.
	type entry struct {
		t     config.Topic
		order int
	}
	grouped := map[string][]entry{}
	for i, t := range c.cfg.Topics {
		grouped[t.Topic] = append(grouped[t.Topic], entry{t: t, order: i})
	}
	for topic, entries := range grouped {
		mc.Subscribe(topic, 0, func(_ mqtt.Client, m mqtt.Message) {
			for _, e := range entries {
				val, err := decodeValue(m.Payload(), e.t.Field)
				if err != nil {
					log.Printf("mqtt decode %s field=%q: %v", m.Topic(), e.t.Field, err)
					continue
				}
				key := e.t.Topic
				if e.t.Field != "" {
					key = e.t.Topic + "#" + e.t.Field
				}
				c.store.SetSensor(state.Sensor{
					Key:        key,
					Topic:      e.t.Topic,
					Name:       e.t.Name,
					Unit:       e.t.Unit,
					Group:      e.t.Group,
					Value:      val,
					Decimals:   e.t.Decimals,
					Order:      e.order,
					StaleAfter: e.t.StaleAfter,
				})
			}
		})
	}
}
