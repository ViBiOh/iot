package hue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ViBiOh/iot/provider"
)

var (
	// WebSocketPrefix ws message prefix for all hue commands
	WebSocketPrefix = []byte(`hue `)

	// GroupsPrefix ws message prefix for groups command
	GroupsPrefix = []byte(`groups `)

	// SchedulesPrefix ws message prefix for schedules command
	SchedulesPrefix = []byte(`schedules `)

	// StatePrefix ws message prefix for state command
	StatePrefix = []byte(`state `)

	// CreatePrefix ws message prefix for create command
	CreatePrefix = []byte(`create `)

	// UpdatePrefix ws message prefix for update command
	UpdatePrefix = []byte(`update `)

	// DeletePrefix ws message prefix for delete command
	DeletePrefix = []byte(`delete `)

	// States available states of lights
	States = map[string]map[string]interface{}{
		`off`: {
			`on`:             false,
			`transitiontime`: 30,
		},
		`on`: {
			`on`:             true,
			`transitiontime`: 30,
			`sat`:            0,
			`bri`:            254,
		},
		`dimmed`: {
			`on`:             true,
			`transitiontime`: 30,
			`sat`:            0,
			`bri`:            0,
		},
		`long_on`: {
			`on`:             true,
			`transitiontime`: 3000,
			`sat`:            0,
			`bri`:            254,
		},
	}
)

// Data stores data fo hub
type Data struct {
	Groups    map[string]*Group
	Schedules map[string]*Schedule
	States    map[string]map[string]interface{}
}

// App stores informations and secret of API
type App struct {
	hub       provider.Hub
	groups    map[string]*Group
	schedules map[string]*Schedule
}

// NewApp creates new App from Flags' config
func NewApp() *App {
	return &App{}
}

func (a *App) sendToWorker(payload []byte) bool {
	return a.hub.SendToWorker(append(WebSocketPrefix, payload...))
}

// Handler create Handler with given App context
func (a *App) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, `/state`) {
			params := r.URL.Query()

			group := params.Get(`group`)
			state := params.Get(`value`)

			if !a.sendToWorker(append(StatePrefix, []byte(fmt.Sprintf(`%s|%s`, group, state))...)) {
				a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: `[hue] Error while sending command to Worker`})
			} else {
				a.hub.RenderDashboard(w, r, http.StatusOK, &provider.Message{Level: `success`, Content: fmt.Sprintf(`%s is now %s`, a.groups[group].Name, state)})
			}

			return
		}

		if strings.HasPrefix(r.URL.Path, `/schedules`) {
			if r.Method == http.MethodPost {
				if r.FormValue(`method`) == http.MethodPost {
					config := &ScheduleConfig{
						Name:      r.FormValue(`name`),
						Group:     r.FormValue(`group`),
						Localtime: ComputeScheduleReccurence(r.Form[`days[]`], r.FormValue(`hours`), r.FormValue(`minutes`)),
						State:     r.FormValue(`state`),
					}

					if payload, err := json.Marshal(config); err != nil {
						a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: fmt.Sprintf(`[hue] Error while marshalling schedule config: %v`, err)})
					} else if !a.sendToWorker(append(SchedulesPrefix, append(CreatePrefix, payload...)...)) {
						a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: `[hue] Error while sending command to Worker`})
					} else {
						a.hub.RenderDashboard(w, r, http.StatusOK, &provider.Message{Level: `success`, Content: fmt.Sprintf(`%s schedule has been created`, config.Name)})
					}

					return
				}

				if r.FormValue(`method`) == http.MethodDelete {
					if !a.sendToWorker(append(SchedulesPrefix, append(DeletePrefix, []byte(strings.TrimPrefix(r.URL.Path, `/schedules/`))...)...)) {
						a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: `[hue] Error while sending command to Worker`})
					} else {
						a.hub.RenderDashboard(w, r, http.StatusOK, &provider.Message{Level: `success`, Content: fmt.Sprintf(`%s schedule has been deleted`, r.FormValue(`name`))})
					}

					return
				}
			} else if r.Method == http.MethodGet {
				parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, `/schedules`), `/`), `/`)

				if len(parts) != 2 {
					a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: fmt.Sprintf(`[hue] Invalid request for updating schedules: %v`, strings.Trim(strings.TrimPrefix(r.URL.Path, `/schedules`), `/`))})
					return
				}

				schedule := &Schedule{
					ID: parts[0],
					APISchedule: &APISchedule{
						Status: parts[1],
					},
				}

				if payload, err := json.Marshal(schedule); err != nil {
					a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: fmt.Sprintf(`[hue] Error while marshalling schedule: %v`, err)})
				} else if !a.sendToWorker(append(SchedulesPrefix, append(UpdatePrefix, payload...)...)) {
					a.hub.RenderDashboard(w, r, http.StatusInternalServerError, &provider.Message{Level: `error`, Content: `[hue] Error while sending command to Worker`})
				} else {
					a.hub.RenderDashboard(w, r, http.StatusOK, &provider.Message{Level: `success`, Content: fmt.Sprintf(`%s is now %s`, a.schedules[parts[0]].Name, parts[1])})
				}

				return
			}
		}

		a.hub.RenderDashboard(w, r, http.StatusServiceUnavailable, &provider.Message{Level: `error`, Content: `[hue] Unknown command`})
	})
}

// SetHub receive Hub during init of it
func (a *App) SetHub(hub provider.Hub) {
	a.hub = hub
}

// GetWorkerPrefix get prefix of message in websocket
func (a *App) GetWorkerPrefix() []byte {
	return WebSocketPrefix
}

// GetData return data for Dashboard rendering
func (a *App) GetData() interface{} {
	return &Data{
		Groups:    a.groups,
		Schedules: a.schedules,
		States:    States,
	}
}

// WorkerHandler handle commands receive from worker
func (a *App) WorkerHandler(payload []byte) error {
	if bytes.HasPrefix(payload, GroupsPrefix) {
		var newGroups map[string]*Group

		if err := json.Unmarshal(bytes.TrimPrefix(payload, GroupsPrefix), &newGroups); err != nil {
			return fmt.Errorf(`[hue] Error while unmarshalling groups: %v`, err)
		}

		a.groups = newGroups

		return nil
	}

	if bytes.HasPrefix(payload, SchedulesPrefix) {
		var newSchedule map[string]*Schedule

		if err := json.Unmarshal(bytes.TrimPrefix(payload, SchedulesPrefix), &newSchedule); err != nil {
			return fmt.Errorf(`[hue] Error while unmarshalling schedules: %v`, err)
		}

		a.schedules = newSchedule

		return nil
	}

	return fmt.Errorf(`[hue] Unknown command`)
}
