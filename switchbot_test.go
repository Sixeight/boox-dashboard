package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestSwitchBotSign(t *testing.T) {
	// Expected value computed with: printf 'tok1792821900000nonce-1' | openssl dgst -sha256 -hmac sec -binary | base64 | tr a-z A-Z
	got := switchBotSign("tok", "sec", 1792821900000, "nonce-1")
	if want := "FQIHJO/E2ECXY3VKYX3U8WMZZHOQ76MPLDKXYR1QZFS="; got != want {
		t.Errorf("switchBotSign = %q, want %q", got, want)
	}
}

const deviceListJSON = `{"statusCode":100,"message":"success","body":{
  "deviceList":[
    {"deviceId":"M1","deviceName":"寝室","deviceType":"Meter","enableCloudService":true},
    {"deviceId":"C1","deviceName":"リビング","deviceType":"MeterPro(CO2)","enableCloudService":true},
    {"deviceId":"H1","deviceName":"Hub 2","deviceType":"Hub 2","enableCloudService":true},
    {"deviceId":"P1","deviceName":"リビング プラス","deviceType":"MeterPlus","enableCloudService":true},
    {"deviceId":"B1","deviceName":"照明ボット","deviceType":"Bot","enableCloudService":true},
    {"deviceId":"M2","deviceName":"クラウド無効","deviceType":"Meter","enableCloudService":false}
  ],
  "infraredRemoteList":[]}}`

var statusJSON = map[string]string{
	"M1": `{"statusCode":100,"message":"success","body":{"deviceId":"M1","deviceType":"Meter","temperature":22.5,"humidity":48,"battery":100}}`,
	"C1": `{"statusCode":100,"message":"success","body":{"deviceId":"C1","deviceType":"MeterPro(CO2)","temperature":24.1,"humidity":40,"CO2":812,"battery":60}}`,
	"H1": `{"statusCode":100,"message":"success","body":{"deviceId":"H1","deviceType":"Hub 2","temperature":23.0,"humidity":45,"lightLevel":12}}`,
	// A sensor that lost contact reports every value as zero.
	"P1": `{"statusCode":100,"message":"success","body":{"deviceId":"P1","deviceType":"MeterPlus","temperature":0,"humidity":0,"battery":0}}`,
}

func TestFetchRoom(t *testing.T) {
	var requested []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "tok" || r.Header.Get("sign") == "" || r.Header.Get("t") == "" || r.Header.Get("nonce") == "" {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		requested = append(requested, r.URL.Path)
		if r.URL.Path == "/v1.1/devices" {
			w.Write([]byte(deviceListJSON))
			return
		}
		for id, body := range statusJSON {
			if r.URL.Path == "/v1.1/devices/"+id+"/status" {
				w.Write([]byte(body))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	c := &switchBotClient{http: upstream.Client(), baseURL: upstream.URL, token: "tok", secret: "sec", now: time.Now}
	got, err := c.fetchRoom()
	if err != nil {
		t.Fatal(err)
	}

	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	want := []Sensor{
		{Name: "寝室", Temperature: f(22.5), Humidity: i(48), Battery: i(100)},
		{Name: "リビング", Temperature: f(24.1), Humidity: i(40), CO2: i(812), Battery: i(60)},
		{Name: "Hub 2", Temperature: f(23.0), Humidity: i(45)},
		{Name: "リビング プラス", Offline: true},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Errorf("fetchRoom = %s, want %s", g, w)
	}
	for _, p := range requested {
		if p == "/v1.1/devices/B1/status" || p == "/v1.1/devices/M2/status" {
			t.Errorf("requested status of a non-sensor or cloud-disabled device: %s", p)
		}
	}
}

func TestFetchRoomAPIError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"statusCode":190,"message":"System error","body":{}}`))
	}))
	defer upstream.Close()

	c := &switchBotClient{http: upstream.Client(), baseURL: upstream.URL, token: "tok", secret: "sec", now: time.Now}
	if _, err := c.fetchRoom(); err == nil {
		t.Error("fetchRoom succeeded on statusCode 190, want error")
	}
}
