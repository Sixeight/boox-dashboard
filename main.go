package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

//go:embed web
var webFS embed.FS

const (
	weatherTTL = 10 * time.Minute
	roomTTL    = 5 * time.Minute
)

func envFloat(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return fallback
	}
	return v
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// jsonHandler serves get's result as JSON, or logs the error under name and responds with code.
func jsonHandler[T any](name string, code int, get func() (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := get()
		if err != nil {
			log.Printf("%s: %v", name, err)
			http.Error(w, name+" unavailable", code)
			return
		}
		writeJSON(w, v)
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	// Keep the home location out of the source: WEATHER_LAT/WEATHER_LON (e.g. in .env) override the city-level default.
	lat := flag.Float64("lat", envFloat("WEATHER_LAT", 35.6812), "latitude for the weather forecast (default: Tokyo)")
	lon := flag.Float64("lon", envFloat("WEATHER_LON", 139.7671), "longitude for the weather forecast")
	webDir := flag.String("web", "", "serve the page from this directory instead of the embedded copy, so page edits apply on reload")
	flag.Parse()

	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	var web fs.FS = os.DirFS(*webDir)
	if *webDir == "" {
		var err error
		if web, err = fs.Sub(webFS, "web"); err != nil {
			log.Fatal(err)
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := openMeteoURL(*lat, *lon)
	weather := &cached[Weather]{ttl: weatherTTL, fetch: func() (Weather, error) { return fetchWeather(client, url) }}

	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web))
	mux.HandleFunc("GET /api/weather", jsonHandler("weather", http.StatusBadGateway, weather.get))
	mux.HandleFunc("GET /api/system", jsonHandler("system", http.StatusInternalServerError, readSystem))

	roomStatus := func() (any, error) { return map[string]any{"enabled": false}, nil }
	if token, secret := os.Getenv("SWITCHBOT_TOKEN"), os.Getenv("SWITCHBOT_SECRET"); token != "" && secret != "" {
		switchBot := &switchBotClient{http: client, baseURL: "https://api.switch-bot.com", token: token, secret: secret, now: time.Now}
		room := &cached[[]Sensor]{ttl: roomTTL, fetch: switchBot.fetchRoom}
		roomStatus = func() (any, error) {
			sensors, err := room.get()
			return map[string]any{"enabled": true, "sensors": sensors}, err
		}
	}
	mux.HandleFunc("GET /api/room", jsonHandler("room", http.StatusBadGateway, roomStatus))

	claudeDir := claudeConfigDir()
	mux.HandleFunc("GET /api/agents", jsonHandler("agents", http.StatusInternalServerError, func() ([]Agent, error) {
		return listAgents(filepath.Join(claudeDir, "sessions"), filepath.Join(claudeDir, "projects"), "/proc", time.Now())
	}))

	log.Printf("listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
