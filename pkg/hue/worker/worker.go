package hue

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/ViBiOh/httputils/v3/pkg/flags"
	"github.com/ViBiOh/httputils/v3/pkg/logger"
	"github.com/ViBiOh/iot/pkg/hue"
	"github.com/ViBiOh/iot/pkg/provider"
)

var (
	_ provider.Worker        = &App{}
	_ provider.Pinger        = &App{}
	_ provider.WorkerHandler = &App{}
	_ provider.Starter       = &App{}
)

// Config of package
type Config struct {
	bridgeIP *string
	username *string
	config   *string
}

// App of package
type App struct {
	bridgeURL      string
	bridgeUsername string
	config         *hueConfig
}

// Flags adds flags for configuring package
func Flags(fs *flag.FlagSet, prefix string) Config {
	return Config{
		bridgeIP: flags.New(prefix, "hue").Name("BridgeIP").Default("").Label("IP of Bridge").ToString(fs),
		username: flags.New(prefix, "hue").Name("Username").Default("").Label("Username for Bridge").ToString(fs),
		config:   flags.New(prefix, "hue").Name("Config").Default("").Label("Configuration filename").ToString(fs),
	}
}

// New creates new App from Config
func New(config Config) (*App, error) {
	username := *config.username

	app := &App{
		bridgeUsername: username,
		bridgeURL:      fmt.Sprintf("http://%s/api/%s", *config.bridgeIP, username),
	}

	if *config.config != "" {
		rawConfig, err := ioutil.ReadFile(*config.config)
		if err != nil {
			return app, err
		}

		if err := json.Unmarshal(rawConfig, &app.config); err != nil {
			return app, err
		}
	}

	return app, nil
}

// Enabled checks if worker is enabled
func (a *App) Enabled() bool {
	return a.bridgeUsername != ""
}

// Start the App
func (a *App) Start() {
	if a.config == nil {
		logger.Warn("no config init for hue")
		return
	}

	ctx := context.Background()

	if err := a.cleanSchedules(ctx); err != nil {
		logger.Error("%s", err)
	}

	if err := a.cleanRules(ctx); err != nil {
		logger.Error("%s", err)
	}

	if err := a.cleanScenes(ctx); err != nil {
		logger.Error("%s", err)
	}

	a.configureSchedules(ctx, a.config.Schedules)
	a.configureTap(ctx, a.config.Taps)
	a.configureMotionSensor(ctx, a.config.Sensors)
}

// GetSource returns source name
func (a *App) GetSource() string {
	return hue.Source
}

// Handle handle worker requests for Hue
func (a *App) Handle(ctx context.Context, p provider.WorkerMessage) (provider.WorkerMessage, error) {
	if strings.HasPrefix(p.Action, hue.WorkerGroupsAction) {
		return a.workerListGroups(ctx, p)
	}

	if strings.HasPrefix(p.Action, hue.WorkerScenesAction) {
		return a.workerListScenes(ctx, p)
	}

	if strings.HasPrefix(p.Action, hue.WorkerSensorsAction) {
		return a.workerListSensors(ctx, p)
	}

	if strings.HasPrefix(p.Action, hue.WorkerSchedulesAction) {
		if err := a.handleSchedules(ctx, p); err != nil {
			return provider.EmptyWorkerMessage, err
		}

		return a.workerListSchedules(ctx, p)
	}

	if strings.HasPrefix(p.Action, hue.WorkerStateAction) {
		if err := a.handleStates(ctx, p); err != nil {
			return provider.EmptyWorkerMessage, err
		}

		return a.workerListGroups(ctx, p)
	}

	return provider.EmptyWorkerMessage, fmt.Errorf("unknown request: %s", p)
}

// Ping send to worker updated data
func (a *App) Ping(ctx context.Context) ([]provider.WorkerMessage, error) {
	pingMessage := provider.NewWorkerMessage(nil, hue.Source, "ping", "")

	groups, err := a.workerListGroups(ctx, pingMessage)
	if err != nil {
		return nil, err
	}

	scenes, err := a.workerListScenes(ctx, pingMessage)
	if err != nil {
		return nil, err
	}

	schedules, err := a.workerListSchedules(ctx, pingMessage)
	if err != nil {
		return nil, err
	}

	sensors, err := a.workerListSensors(ctx, pingMessage)
	if err != nil {
		return nil, err
	}

	return []provider.WorkerMessage{groups, scenes, schedules, sensors}, nil
}

func (a *App) handleStates(ctx context.Context, p provider.WorkerMessage) error {
	if parts := strings.Split(p.Payload, "|"); len(parts) == 2 {
		state, ok := hue.States[parts[1]]
		if !ok {
			return fmt.Errorf("unknown state %s", parts[1])
		}

		if err := a.updateGroupState(ctx, parts[0], state); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("invalid state request: %s", p.Payload)
	}

	return nil
}

func (a *App) handleSchedules(ctx context.Context, p provider.WorkerMessage) error {
	if strings.HasSuffix(p.Action, hue.UpdateAction) {
		var config hue.Schedule

		if err := json.Unmarshal([]byte(p.Payload), &config); err != nil {
			return err
		}

		if config.ID == "" {
			return errors.New("ID is missing")
		}

		return a.updateSchedule(ctx, &config)
	}

	if strings.HasSuffix(p.Action, hue.DeleteAction) {
		id := p.Payload

		schedule, err := a.getSchedule(ctx, id)
		if err != nil {
			return err
		}

		if err := a.deleteSchedule(ctx, id); err != nil {
			return err
		}

		if sceneID, ok := schedule.Command.Body["scene"]; ok {
			if err := a.deleteScene(ctx, sceneID.(string)); err != nil {
				return err
			}
		}

		return nil
	}

	return errors.New("unknown schedule command")
}

func (a *App) workerListGroups(ctx context.Context, initial provider.WorkerMessage) (provider.WorkerMessage, error) {
	groups, err := a.listGroups(ctx)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	payload, err := json.Marshal(groups)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	return provider.NewWorkerMessage(&initial, hue.Source, hue.WorkerGroupsAction, fmt.Sprintf("%s", payload)), nil
}

func (a *App) workerListScenes(ctx context.Context, initial provider.WorkerMessage) (provider.WorkerMessage, error) {
	scenes, err := a.listScenes(ctx)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	payload, err := json.Marshal(scenes)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	return provider.NewWorkerMessage(&initial, hue.Source, hue.WorkerScenesAction, fmt.Sprintf("%s", payload)), nil
}

func (a *App) workerListSchedules(ctx context.Context, initial provider.WorkerMessage) (provider.WorkerMessage, error) {
	schedules, err := a.listSchedules(ctx)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	payload, err := json.Marshal(schedules)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	return provider.NewWorkerMessage(&initial, hue.Source, hue.WorkerSchedulesAction, fmt.Sprintf("%s", payload)), nil
}

func (a *App) workerListSensors(ctx context.Context, initial provider.WorkerMessage) (provider.WorkerMessage, error) {
	sensors, err := a.listSensors(ctx)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	payload, err := json.Marshal(sensors)
	if err != nil {
		return provider.EmptyWorkerMessage, err
	}

	return provider.NewWorkerMessage(&initial, hue.Source, hue.WorkerSensorsAction, fmt.Sprintf("%s", payload)), nil
}
