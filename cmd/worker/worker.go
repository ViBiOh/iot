package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"time"

	"github.com/ViBiOh/httputils/pkg/errors"
	"github.com/ViBiOh/httputils/pkg/logger"
	"github.com/ViBiOh/httputils/pkg/opentracing"
	"github.com/ViBiOh/httputils/pkg/tools"
	hue_worker "github.com/ViBiOh/iot/pkg/hue/worker"
	"github.com/ViBiOh/iot/pkg/mqtt"
	netatmo_worker "github.com/ViBiOh/iot/pkg/netatmo/worker"
	"github.com/ViBiOh/iot/pkg/provider"
	sonos_worker "github.com/ViBiOh/iot/pkg/sonos/worker"
)

const (
	pingDelay = 60 * time.Second
)

// App of package
type App struct {
	workers    map[string]provider.Worker
	mqttClient *mqtt.App
}

// New creates new App from Config
func New(workers []provider.Worker, mqttClient *mqtt.App) *App {
	workersMap := make(map[string]provider.Worker, len(workers))
	for _, worker := range workers {
		workersMap[worker.GetSource()] = worker
	}

	return &App{
		workers:    workersMap,
		mqttClient: mqttClient,
	}
}

func (a *App) pingWorkers() {
	ctx := context.Background()
	workersCount := len(a.workers)

	inputs, results, errors := tools.ConcurrentAction(uint(workersCount), func(e interface{}) (interface{}, error) {
		if worker, ok := e.(provider.Worker); ok {
			return worker.Ping(ctx)
		}

		return nil, errors.New(`unrecognized worker type: %+v`, e)
	})

	go func() {
		defer close(inputs)

		for _, worker := range a.workers {
			inputs <- worker
		}
	}()

	for i := 0; i < workersCount; i++ {
		select {
		case err := <-errors:
			logger.Error(`%+v`, err)
			break

		case result := <-results:
			for _, message := range result.([]*provider.WorkerMessage) {
				if err := provider.WriteMessage(ctx, a.mqttClient, `message_from_worker`, message); err != nil {
					logger.Error(`%+v`, err)
				}
			}

			break
		}
	}
}

func (a *App) pinger() {
	for {
		a.pingWorkers()
		time.Sleep(pingDelay)
	}
}

func (a *App) handleTextMessage(p []byte) {
	var message provider.WorkerMessage
	if err := json.Unmarshal(p, &message); err != nil {
		logger.Error(`%+v`, errors.WithStack(err))
		return
	}

	ctx, span, err := opentracing.ExtractSpanFromMap(context.Background(), message.Tracing, message.Action)
	if err != nil {
		logger.Error(`%+v`, errors.WithStack(err))
	}
	if span != nil {
		defer span.Finish()
	}

	if worker, ok := a.workers[message.Source]; ok {
		output, err := worker.Handle(ctx, &message)

		if err != nil {
			logger.Error(`%+v`, err)
		}

		if output != nil {
			if err := provider.WriteMessage(ctx, a.mqttClient, `message_from_worker`, output); err != nil {
				logger.Error(`%+v`, err)
			}
		}

		return
	}

	logger.Error(`unknown request: %s`, message)
}

func (a *App) connect() {
	err := a.mqttClient.Subscribe(`message_to_worker`, a.handleTextMessage)
	if err != nil {
		logger.Error(`%+v`, err)
	}

	a.pinger()
}

func main() {
	fs := flag.NewFlagSet(`iot-worker`, flag.ExitOnError)

	mqttConfig := mqtt.Flags(fs, `mqtt`)
	hueConfig := hue_worker.Flags(fs, `hue`)
	netatmoConfig := netatmo_worker.Flags(fs, `netatmo`)
	sonosConfig := sonos_worker.Flags(fs, `sonos`)

	if err := fs.Parse(os.Args[1:]); err != nil {
		logger.Fatal(`%+v`, err)
	}

	hueApp, err := hue_worker.New(hueConfig)
	if err != nil {
		logger.Error(`%+v`, err)
		os.Exit(1)
	}

	mqttApp, err := mqtt.New(mqttConfig)
	if err != nil {
		logger.Fatal(`%+v`, err)
	}

	netatmoApp := netatmo_worker.New(netatmoConfig)
	sonosApp := sonos_worker.New(sonosConfig)
	app := New([]provider.Worker{hueApp, netatmoApp, sonosApp}, mqttApp)

	app.connect()
}
