/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package StarterMQTT

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go-spring.org/cloud/resilience"
	"go-spring.org/log"
	"go-spring.org/spring/conf"
	"go-spring.org/spring/gs"
	"go-spring.org/stdlib/errutil"
	"go-spring.org/stdlib/flatten"
)

var starterTag = log.RegisterInfraTag("mqtt", "")

func init() {

	// Register multiple MQTT clients as a group.
	// Each instance is created according to the configuration in "${spring.mqtt}".
	// This allows defining multiple MQTT clients dynamically.
	gs.Module(gs.OnProperty("spring.mqtt"), func(r gs.BeanProvider, p flatten.Storage) error {
		return conf.BindEach(p, "${spring.mqtt}", func(name string, c Config) error {
			r.Provide(newClient,
				gs.IndexArg(1, gs.ValueArg(name)),
				gs.IndexArg(2, gs.ValueArg(c)),
			).Name(name).Destroy(destroyClient).Caller(1)
			return nil
		})
	})
}

// newClient creates and connects an MQTT client based on the provided configuration.
func newClient(ctx *gs.ContextProvider, name string, c Config) (mqtt.Client, error) {
	log.Debugf(ctx.Context, starterTag, "creating mqtt client, broker=%s client-id=%s", c.Broker, c.ClientID)

	opts := mqtt.NewClientOptions().
		AddBroker(c.Broker).
		SetClientID(c.ClientID).
		SetUsername(c.Username).
		SetPassword(c.Password).
		SetCleanSession(c.CleanSession).
		SetKeepAlive(c.KeepAlive).
		SetConnectTimeout(c.ConnectTimeout)

	// Bridge connection-lifecycle events into go-spring's log so the client's
	// health (default auto-reconnect stays on) shows up alongside app logs.
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		log.Infof(ctx.Context, log.TagAppDef, "mqtt connected to %q", c.Broker)
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Warnf(ctx.Context, log.TagAppDef, "mqtt connection lost: %v", err)
	})
	opts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
		log.Infof(ctx.Context, log.TagAppDef, "mqtt reconnecting to %q", c.Broker)
	})

	tlsCfg, err := c.TLS.Build()
	if err != nil {
		log.Errorf(ctx.Context, starterTag, "mqtt: build TLS failed: %v", err)
		return nil, errutil.Explain(err, "mqtt: build TLS")
	}
	if tlsCfg != nil {
		opts.SetTLSConfig(tlsCfg)
	}

	if c.Will.Topic != "" {
		opts.SetWill(c.Will.Topic, c.Will.Payload, c.Will.QoS, c.Will.Retained)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		log.Errorf(ctx.Context, starterTag, "mqtt: connect failed broker=%s: %v", c.Broker, err)
		return nil, err
	}
	if err := applyResilience(c, client, resilience.ResourceLabel("mqtt", c.Broker)); err != nil {
		log.Errorf(ctx.Context, starterTag, "mqtt: resilience setup failed: %v", err)
		client.Disconnect(250)
		return nil, err
	}
	log.Infof(ctx.Context, starterTag, "mqtt client initialized, broker=%s", c.Broker)
	return client, nil
}

// destroyClient disconnects the MQTT client, waiting up to 250ms for
// in-flight work to complete. When a resilience executor is attached its Close
// releases any background resources of a production driver.
func destroyClient(client mqtt.Client) error {
	closeResilience(client)
	client.Disconnect(250)
	return nil
}
