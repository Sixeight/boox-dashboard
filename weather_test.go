package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWeatherLabel(t *testing.T) {
	cases := map[int]string{
		0:  "快晴",
		2:  "晴れ時々曇り",
		3:  "曇り",
		45: "霧",
		63: "雨",
		71: "雪",
		95: "雷雨",
		-1: "不明",
	}
	for code, want := range cases {
		if got := weatherLabel(code); got != want {
			t.Errorf("weatherLabel(%d) = %q, want %q", code, got, want)
		}
	}
}

const forecastJSON = `{
  "current": {"temperature_2m": 15.6, "weather_code": 63},
  "daily": {
    "time": ["2026-10-24", "2026-10-25"],
    "weather_code": [63, 1],
    "temperature_2m_max": [18.2, 21.0],
    "temperature_2m_min": [12.1, 13.4],
    "precipitation_probability_max": [90, 10]
  }
}`

func TestFetchWeather(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(forecastJSON))
	}))
	defer upstream.Close()

	w, err := fetchWeather(upstream.Client(), upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if w.Now.Temp != 15.6 || w.Now.Label != "雨" {
		t.Errorf("Now = %+v", w.Now)
	}
	if len(w.Days) != 2 {
		t.Fatalf("len(Days) = %d, want 2", len(w.Days))
	}
	want := Day{Date: "2026-10-25", Label: "晴れ", Max: 21.0, Min: 13.4, Pop: 10}
	if w.Days[1] != want {
		t.Errorf("Days[1] = %+v, want %+v", w.Days[1], want)
	}
}
