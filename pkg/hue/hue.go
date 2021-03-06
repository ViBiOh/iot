package hue

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/ViBiOh/httputils/v4/pkg/flags"
	"github.com/ViBiOh/httputils/v4/pkg/renderer"
	"github.com/prometheus/client_golang/prometheus"
)

// App stores informations and secret of API
type App interface {
	Handler() http.Handler
	TemplateFunc(http.ResponseWriter, *http.Request) (string, int, map[string]interface{}, error)
	Start(<-chan struct{})
}

// Config of package
type Config struct {
	bridgeIP       *string
	bridgeUsername *string
	config         *string
}

type app struct {
	prometheusRegisterer prometheus.Registerer
	prometheusCollectors map[string]prometheus.Gauge

	config      *configHue
	apiHandler  http.Handler
	rendererApp renderer.App

	groups    map[string]Group
	scenes    map[string]Scene
	schedules map[string]Schedule
	sensors   map[string]Sensor

	bridgeURL      string
	bridgeUsername string

	mutex sync.RWMutex
}

// Flags adds flags for configuring package
func Flags(fs *flag.FlagSet, prefix string) Config {
	return Config{
		bridgeIP:       flags.New(prefix, "hue").Name("BridgeIP").Default("").Label("IP of Bridge").ToString(fs),
		bridgeUsername: flags.New(prefix, "hue").Name("Username").Default("").Label("Username for Bridge").ToString(fs),
		config:         flags.New(prefix, "hue").Name("Config").Default("").Label("Configuration filename").ToString(fs),
	}
}

// New creates new App from Config
func New(config Config, registerer prometheus.Registerer, renderer renderer.App) (App, error) {
	bridgeUsername := strings.TrimSpace(*config.bridgeUsername)

	app := &app{
		bridgeURL:      fmt.Sprintf("http://%s/api/%s", strings.TrimSpace(*config.bridgeIP), bridgeUsername),
		bridgeUsername: bridgeUsername,

		rendererApp: renderer,

		prometheusRegisterer: registerer,
		prometheusCollectors: make(map[string]prometheus.Gauge),
	}

	app.apiHandler = http.StripPrefix(apiPath, app.Handler())

	configFile := strings.TrimSpace(*config.config)
	if len(configFile) != 0 {
		rawConfig, err := os.ReadFile(configFile)
		if err != nil {
			return app, err
		}

		if err := json.Unmarshal(rawConfig, &app.config); err != nil {
			return app, err
		}
	}

	return app, nil
}

func (a *app) TemplateFunc(w http.ResponseWriter, r *http.Request) (string, int, map[string]interface{}, error) {
	if strings.HasPrefix(r.URL.Path, apiPath) {
		a.apiHandler.ServeHTTP(w, r)
		return "", 0, nil, nil
	}

	a.mutex.RLock()
	defer a.mutex.RUnlock()

	return "public", http.StatusOK, map[string]interface{}{
		"Groups":    a.groups,
		"Scenes":    a.scenes,
		"Schedules": a.schedules,
		"Sensors":   a.sensors,
	}, nil
}
