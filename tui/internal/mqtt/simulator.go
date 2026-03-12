package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// payload is the JSON structure published to the MQTT broker.
type payload struct {
	Temperature  float64 `json:"temperature"`
	Humidity     float64 `json:"humidity"`
	Pressure     float64 `json:"pressure"`
	BatteryValue int     `json:"battery_value"`
	Tbu          float64 `json:"tbu"`
	State        string  `json:"state"`
	ErrorCode    int     `json:"error_code"`
	RSSI         int     `json:"rssi"`
}

func randomPayload(r *rand.Rand) payload {
	temp := r.Float64()*50 - 10      // -10..40
	hum := r.Float64()*60 + 30       // 30..90
	pres := r.Float64()*50 + 980     // 980..1030
	bat := r.Intn(71) + 30           // 30..100
	tbu := r.Float64() * 50          // 0..50
	rssi := -(r.Intn(61) + 40)       // -100..-40

	state := "NORMAL"
	if temp < 2 {
		state = "HIGH"
	} else if bat < 20 {
		state = "LOW"
	}
	return payload{
		Temperature:  round2(temp),
		Humidity:     round2(hum),
		Pressure:     round2(pres),
		BatteryValue: bat,
		Tbu:          round2(tbu),
		State:        state,
		ErrorCode:    0,
		RSSI:         rssi,
	}
}

// round2 rounds to 2 decimal places.
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func macFromIndex(i int) string {
	return fmt.Sprintf("AA:BB:CC:DD:%02X:%02X", (i>>8)&0xFF, i&0xFF)
}

// DeviceSimulator simulates a single MQTT device.
type DeviceSimulator struct {
	Index    int
	Host     string
	Port     string
	Username string
	Password string
	Lines    chan string

	cancel context.CancelFunc
	done   chan struct{}
	r      *rand.Rand
}

// NewDeviceSimulator creates a DeviceSimulator with the given MQTT credentials.
func NewDeviceSimulator(index int, host, port, username, password string, lines chan string) *DeviceSimulator {
	return &DeviceSimulator{
		Index:    index,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		Lines:    lines,
		done:     make(chan struct{}),
		r:        rand.New(rand.NewSource(time.Now().UnixNano() + int64(index))),
	}
}

// Start connects to the broker and publishes every second until ctx is cancelled.
func (d *DeviceSimulator) Start(ctx context.Context) {
	mac := macFromIndex(d.Index)
	topic := fmt.Sprintf("agrisense/devices/%s/data", mac)

	broker := fmt.Sprintf("tcp://%s:%s", d.Host, d.Port)
	opts := paho.NewClientOptions().
		AddBroker(broker).
		SetClientID(fmt.Sprintf("agrisense-sim-%d", d.Index)).
		SetConnectTimeout(5 * time.Second).
		SetAutoReconnect(true)
	if d.Username != "" {
		opts.SetUsername(d.Username)
		opts.SetPassword(d.Password)
	}

	client := paho.NewClient(opts)

	send := func(line string) {
		select {
		case d.Lines <- line:
		default:
		}
	}

	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		send(fmt.Sprintf("[%s] connect error: %v", mac, tok.Error()))
		close(d.done)
		return
	}
	send(fmt.Sprintf("[%s] connected to %s", mac, broker))

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			client.Disconnect(500)
			send(fmt.Sprintf("[%s] disconnected", mac))
			close(d.done)
			return
		case <-ticker.C:
			p := randomPayload(d.r)
			data, _ := json.Marshal(p)
			if tok := client.Publish(topic, 0, false, data); tok.Wait() && tok.Error() != nil {
				send(fmt.Sprintf("[%s] publish error: %v", mac, tok.Error()))
				continue
			}
			send(fmt.Sprintf("[%s] → %.1f°C | %.0f%% | %ddBm | %s",
				mac, p.Temperature, p.Humidity, p.RSSI, p.State))
		}
	}
}

// Stop cancels the device simulator.
func (d *DeviceSimulator) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

// SwarmSimulator simulates multiple MQTT devices concurrently.
type SwarmSimulator struct {
	NumDevices  int
	NumRequests int // 0 = infinite
	Host        string
	Port        string
	Username    string
	Password    string
	Lines       chan string

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewSwarmSimulator creates a SwarmSimulator.
func NewSwarmSimulator(numDevices, numRequests int, host, port, username, password string, lines chan string) *SwarmSimulator {
	return &SwarmSimulator{
		NumDevices:  numDevices,
		NumRequests: numRequests,
		Host:        host,
		Port:        port,
		Username:    username,
		Password:    password,
		Lines:       lines,
	}
}

// Start launches one goroutine per device, all sharing a cancellable context.
func (s *SwarmSimulator) Start(ctx context.Context) {
	ctxSim, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	for i := 0; i < s.NumDevices; i++ {
		s.wg.Add(1)
		go func(idx int) {
			defer s.wg.Done()
			s.runDevice(ctxSim, idx)
		}(i)
	}

	go func() {
		s.wg.Wait()
		select {
		case s.Lines <- "[swarm] all devices finished":
		default:
		}
	}()
}

func (s *SwarmSimulator) runDevice(ctx context.Context, index int) {
	mac := macFromIndex(index)
	topic := fmt.Sprintf("agrisense/devices/%s/data", mac)

	broker := fmt.Sprintf("tcp://%s:%s", s.Host, s.Port)
	opts := paho.NewClientOptions().
		AddBroker(broker).
		SetClientID(fmt.Sprintf("agrisense-swarm-%d", index)).
		SetConnectTimeout(5 * time.Second).
		SetAutoReconnect(true)
	if s.Username != "" {
		opts.SetUsername(s.Username)
		opts.SetPassword(s.Password)
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(index)))

	send := func(line string) {
		select {
		case s.Lines <- line:
		default:
		}
	}

	client := paho.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		send(fmt.Sprintf("[Device %d/%d] %s connect error: %v", index+1, s.NumDevices, mac, tok.Error()))
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	sent := 0
	for {
		select {
		case <-ctx.Done():
			client.Disconnect(500)
			return
		case <-ticker.C:
			p := randomPayload(r)
			data, _ := json.Marshal(p)
			if tok := client.Publish(topic, 0, false, data); tok.Wait() && tok.Error() != nil {
				send(fmt.Sprintf("[Device %d/%d] %s publish error: %v", index+1, s.NumDevices, mac, tok.Error()))
				continue
			}
			send(fmt.Sprintf("[Device %d/%d] %s  →  %.1f°C | %.0f%% | %ddBm | %s",
				index+1, s.NumDevices, mac, p.Temperature, p.Humidity, p.RSSI, p.State))
			sent++
			if s.NumRequests > 0 && sent >= s.NumRequests {
				client.Disconnect(500)
				return
			}
		}
	}
}

// Stop cancels all device goroutines and waits for them to finish.
func (s *SwarmSimulator) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}
