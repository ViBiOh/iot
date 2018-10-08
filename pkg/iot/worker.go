package iot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ViBiOh/iot/pkg/provider"
)

const (
	workerWaitDelay = 10 * time.Second
)

func (a *App) registerWorker(worker provider.WorkerProvider) {
	a.workerProviders[worker.GetWorkerSource()] = worker
}

// SendToWorker sends payload to worker
func (a *App) SendToWorker(ctx context.Context, root *provider.WorkerMessage, source, action string, payload interface{}, waitOutput bool) *provider.WorkerMessage {
	message := provider.NewWorkerMessage(root, source, action, fmt.Sprintf(`%s`, payload))

	var outputChan chan *provider.WorkerMessage
	if waitOutput {
		outputChan = make(chan *provider.WorkerMessage)
		a.workerCalls.Store(message.ID, outputChan)

		defer a.workerCalls.Delete(message.ID)
	}

	if err := provider.WriteMessage(ctx, a.wsConn, message); err != nil {
		return provider.NewWorkerMessage(root, message.Source, provider.WorkerErrorAction, err)
	}

	if waitOutput {
		select {
		case output := <-outputChan:
			return output
		case <-time.After(workerWaitDelay):
			return provider.NewWorkerMessage(root, message.Source, provider.WorkerErrorAction, errors.New(`timeout exceeded`))
		}
	}

	return nil
}
