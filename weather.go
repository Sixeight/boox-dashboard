package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Weather struct {
	Now  Now   `json:"now"`
	Days []Day `json:"days"`
}

type Now struct {
	Temp  float64 `json:"temp"`
	Label string  `json:"label"`
}

type Day struct {
	Date  string  `json:"date"`
	Label string  `json:"label"`
	Max   float64 `json:"max"`
	Min   float64 `json:"min"`
	Pop   int     `json:"pop"`
}

func openMeteoURL(lat, lon float64) string {
	return fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f"+
		"&current=temperature_2m,weather_code"+
		"&daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"+
		"&timezone=Asia%%2FTokyo&forecast_days=3", lat, lon)
}

func fetchWeather(client *http.Client, url string) (Weather, error) {
	resp, err := client.Get(url)
	if err != nil {
		return Weather{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Weather{}, fmt.Errorf("open-meteo: %s", resp.Status)
	}

	var body struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Code int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			Time []string  `json:"time"`
			Code []int     `json:"weather_code"`
			Max  []float64 `json:"temperature_2m_max"`
			Min  []float64 `json:"temperature_2m_min"`
			Pop  []int     `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Weather{}, err
	}

	d := body.Daily
	if len(d.Code) != len(d.Time) || len(d.Max) != len(d.Time) || len(d.Min) != len(d.Time) || len(d.Pop) != len(d.Time) {
		return Weather{}, fmt.Errorf("open-meteo: daily arrays have mismatched lengths")
	}
	w := Weather{Now: Now{Temp: body.Current.Temp, Label: weatherLabel(body.Current.Code)}}
	for i, date := range d.Time {
		w.Days = append(w.Days, Day{Date: date, Label: weatherLabel(d.Code[i]), Max: d.Max[i], Min: d.Min[i], Pop: d.Pop[i]})
	}
	return w, nil
}

// weatherLabel maps a WMO weather interpretation code to a short Japanese label.
func weatherLabel(code int) string {
	switch {
	case code == 0:
		return "快晴"
	case code == 1:
		return "晴れ"
	case code == 2:
		return "晴れ時々曇り"
	case code == 3:
		return "曇り"
	case code == 45 || code == 48:
		return "霧"
	case code >= 51 && code <= 57:
		return "霧雨"
	case code >= 61 && code <= 67, code >= 80 && code <= 82:
		return "雨"
	case code >= 71 && code <= 77, code == 85 || code == 86:
		return "雪"
	case code >= 95 && code <= 99:
		return "雷雨"
	default:
		return "不明"
	}
}
