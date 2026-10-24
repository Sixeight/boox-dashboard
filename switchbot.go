package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sensor is a SwitchBot device that reports room conditions. Fields a device does not report are nil.
type Sensor struct {
	Name        string   `json:"name"`
	Temperature *float64 `json:"temperature"`
	Humidity    *int     `json:"humidity"`
	CO2         *int     `json:"co2"` // also decodes the API's "CO2": encoding/json matches keys case-insensitively
	Battery     *int     `json:"battery"`
	Offline     bool     `json:"offline"`
}

// switchBotDevicesTTL bounds device-list calls; the list rarely changes and calls count against the 10,000/day limit.
const switchBotDevicesTTL = time.Hour

type switchBotDevice struct {
	ID           string `json:"deviceId"`
	Name         string `json:"deviceName"`
	Type         string `json:"deviceType"`
	CloudEnabled bool   `json:"enableCloudService"`
}

type switchBotClient struct {
	http    *http.Client
	baseURL string
	token   string
	secret  string
	now     func() time.Time

	devices          []switchBotDevice
	devicesFetchedAt time.Time
}

// switchBotSign implements the v1.1 signature: upper(base64(HMAC-SHA256(secret, token + t + nonce))).
func switchBotSign(token, secret string, t int64, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token + strconv.FormatInt(t, 10) + nonce))
	return strings.ToUpper(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

// uuidV4 returns a random RFC 9562 version 4 UUID, the nonce format the API documents.
func uuidV4() string {
	var u [16]byte
	rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
}

func (c *switchBotClient) get(path string, body any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	t := c.now().UnixMilli()
	nonce := uuidV4()
	req.Header.Set("Authorization", c.token)
	req.Header.Set("sign", switchBotSign(c.token, c.secret, t, nonce))
	req.Header.Set("t", strconv.FormatInt(t, 10))
	req.Header.Set("nonce", nonce)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switchbot %s: %s", path, resp.Status)
	}
	var envelope struct {
		StatusCode int             `json:"statusCode"`
		Message    string          `json:"message"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("switchbot %s: %w", path, err)
	}
	if envelope.StatusCode != 100 {
		return fmt.Errorf("switchbot %s: statusCode %d: %s", path, envelope.StatusCode, envelope.Message)
	}
	return json.Unmarshal(envelope.Body, body)
}

func isZero[T int | float64](v *T) bool { return v != nil && *v == 0 }

// isSensorType reports whether a device type returns temperature and humidity in its status.
func isSensorType(deviceType string) bool {
	switch deviceType {
	case "WoIOSensor", "Hub 2", "Hub 3":
		return true
	}
	return strings.Contains(deviceType, "Meter")
}

func (c *switchBotClient) listDevices() ([]switchBotDevice, error) {
	if c.devices != nil && c.now().Sub(c.devicesFetchedAt) < switchBotDevicesTTL {
		return c.devices, nil
	}
	var body struct {
		DeviceList []switchBotDevice `json:"deviceList"`
	}
	if err := c.get("/v1.1/devices", &body); err != nil {
		return nil, err
	}
	c.devices, c.devicesFetchedAt = body.DeviceList, c.now()
	return c.devices, nil
}

// fetchRoom reads every cloud-enabled sensor. A device whose status fails is skipped so one offline sensor does not hide the rest.
func (c *switchBotClient) fetchRoom() ([]Sensor, error) {
	devices, err := c.listDevices()
	if err != nil {
		return nil, err
	}
	sensors := []Sensor{}
	for _, d := range devices {
		if !d.CloudEnabled || !isSensorType(d.Type) {
			continue
		}
		var sensor Sensor
		if err := c.get("/v1.1/devices/"+d.ID+"/status", &sensor); err != nil {
			log.Printf("switchbot: %s: %v", d.Name, err)
			continue
		}
		// A sensor out of contact (dead battery, out of hub range) reports zero for every value instead of an error.
		if isZero(sensor.Temperature) && isZero(sensor.Humidity) && isZero(sensor.Battery) {
			sensor = Sensor{Offline: true}
		}
		sensor.Name = d.Name
		sensors = append(sensors, sensor)
	}
	return sensors, nil
}
