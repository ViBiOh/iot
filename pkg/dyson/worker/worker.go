package worker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ViBiOh/httputils/pkg/logger"
	"github.com/ViBiOh/httputils/pkg/tools"
	"github.com/ViBiOh/iot/pkg/dyson"
	"github.com/ViBiOh/iot/pkg/provider"
	"github.com/pkg/errors"
)

const (
	// API of Dyson Link
	API = `https://api.cp.dyson.com`

	authenticateEndpoint = `/v1/userregistration/authenticate`
	devicesEndpoint      = `/v1/provisioningservice/manifest`
)

var unsafeHTTPClient = http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

// Config of package
type Config struct {
	email    *string
	password *string
	country  *string
	clientID *string
}

// App of package
type App struct {
	account  string
	password string
	clientID string
	devices  []*dyson.Device
}

// Flags adds flags for configuring package
func Flags(fs *flag.FlagSet, prefix string) Config {
	return Config{
		email:    fs.String(tools.ToCamel(fmt.Sprintf(`%sEmail`, prefix)), ``, `[dyson] Link email`),
		password: fs.String(tools.ToCamel(fmt.Sprintf(`%sPassword`, prefix)), ``, `[dyson] Link eassword`),
		country:  fs.String(tools.ToCamel(fmt.Sprintf(`%sCountry`, prefix)), `FR`, `[dyson] Link eountry`),
		clientID: fs.String(tools.ToCamel(fmt.Sprintf(`%sClientID`, prefix)), `iot`, `[dyson] MQTT Client ID`),
	}
}

// New creates new App from Config
func New(config Config) *App {
	email := strings.TrimSpace(*config.email)
	if email == `` {
		logger.Warn(`no email provided`)
		return &App{}
	}

	password := strings.TrimSpace(*config.password)
	if password == `` {
		logger.Warn(`no password provided`)
		return &App{}
	}

	authContent, err := getAuth(email, password, *config.country)
	if err != nil {
		logger.Error(`%+v`, err)
		return &App{}
	}

	app := &App{
		account:  authContent[`Account`],
		password: authContent[`Password`],
		clientID: strings.TrimSpace(*config.clientID),
	}

	return app
}

// Enabled checks if worker is enabled
func (a *App) Enabled() bool {
	return a.account != `` && a.password != `` && a.clientID != ``
}

// Start the package
func (a *App) Start() {
	if !a.Enabled() {
		logger.Warn(`no config provided`)
		return
	}

	devices, err := a.getDevices(nil)
	if err != nil {
		logger.Error(`%+v`, err)
		return
	}

	services, err := findDysonMQTTServices()
	if err != nil {
		logger.Error(`%+v`, err)
		return
	}

	for _, device := range devices {
		if service, ok := services[fmt.Sprintf(`%s_%s`, device.ProductType, device.Serial)]; ok {
			device.Service = service

			if err := device.ConnectToMQTT(a.clientID); err != nil {
				logger.Error(`%+v`, err)
				continue
			}

			if err := device.SubcribeToStatus(); err != nil {
				logger.Error(`%+v`, err)
				continue
			}
		} else {
			logger.Warn(`no service found for %s`, device.Serial)
		}
	}

	a.devices = devices
}

// GetSource returns source name
func (a *App) GetSource() string {
	return dyson.Source
}

// Handle handle worker requests for Dyson
func (a *App) Handle(ctx context.Context, p *provider.WorkerMessage) (*provider.WorkerMessage, error) {
	return nil, nil
}

// Ping send to worker updated data
func (a *App) Ping(ctx context.Context) ([]*provider.WorkerMessage, error) {
	stateMessage, err := dyson.NewCurrentStateMessage()
	if err != nil {
		return nil, err
	}

	workerMessages := make([]*provider.WorkerMessage, 0)

	for _, device := range a.devices {
		if err := device.SendCommand(stateMessage); err != nil {
			return nil, err
		}

		workerMessage, err := a.workerListDevices(ctx, nil)
		if err != nil {
			return nil, err
		}

		workerMessages = append(workerMessages, workerMessage)
	}

	return workerMessages, nil
}

func (a *App) workerListDevices(ctx context.Context, initial *provider.WorkerMessage) (*provider.WorkerMessage, error) {
	payload, err := json.Marshal(a.devices)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return provider.NewWorkerMessage(initial, dyson.Source, dyson.WorkerDevicesAction, fmt.Sprintf(`%s`, payload)), nil
}
