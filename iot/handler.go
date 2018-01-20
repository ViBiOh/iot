package iot

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path"

	"github.com/ViBiOh/auth/auth"
	"github.com/ViBiOh/httputils"
	"github.com/ViBiOh/httputils/tools"
	"github.com/ViBiOh/iot/provider"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// App stores informations and secret of API
type App struct {
	authApp   *auth.App
	tpl       *template.Template
	providers map[string]provider.Provider
	secretKey string
	wsConn    *websocket.Conn
}

// NewApp creates new App from dependencies and Flags' config
func NewApp(config map[string]*string, providers map[string]provider.Provider, authApp *auth.App) *App {
	app := &App{
		authApp:   authApp,
		tpl:       template.Must(template.New(`iot`).ParseGlob(`./web/*.gohtml`)),
		providers: providers,
		secretKey: *config[`secretKey`],
	}

	for _, provider := range providers {
		provider.SetHub(app)
	}

	return app
}

// Flags add flags for given prefix
func Flags(prefix string) map[string]*string {
	return map[string]*string{
		`secretKey`: flag.String(tools.ToCamel(prefix+`SecretKey`), ``, `[iot] Secret Key between worker and API`),
	}
}

func (a *App) checkWorker(ws *websocket.Conn) bool {
	messageType, p, err := ws.ReadMessage()

	if err != nil {
		provider.WriteErrorMessage(ws, fmt.Errorf(`Error while reading first message: %v`, err))
		return false
	}

	if messageType != websocket.TextMessage {
		provider.WriteErrorMessage(ws, errors.New(`First message should be a Text Message`))
		return false
	}

	if string(p) != a.secretKey {
		provider.WriteErrorMessage(ws, errors.New(`First message should be the Secret Key`))
		return false
	}

	return true
}

// SendToWorker sends payload to worker
func (a *App) SendToWorker(payload []byte) bool {
	return provider.WriteTextMessage(a.wsConn, payload)
}

// RenderDashboard render dashboard
func (a *App) RenderDashboard(w http.ResponseWriter, r *http.Request, status int, message *provider.Message) {
	response := map[string]interface{}{
		`Online`:  a.wsConn != nil,
		`Message`: message,
	}

	for name, provider := range a.providers {
		response[name] = provider.GetData()
	}

	w.Header().Add(`Cache-Control`, `no-cache`)
	if err := httputils.WriteHTMLTemplate(a.tpl.Lookup(`iot`), w, response, status); err != nil {
		httputils.InternalServerError(w, err)
	}
}

// WebsocketHandler create Websockethandler
func (a *App) WebsocketHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if ws != nil {
			defer func() {
				if a.wsConn == ws {
					a.wsConn = nil
				}

				ws.Close()
			}()
		}
		if err != nil {
			log.Printf(`Error while upgrading connection: %v`, err)
			return
		}

		if !a.checkWorker(ws) {
			return
		}

		log.Printf(`Worker connection from %s`, httputils.GetIP(r))
		if a.wsConn != nil {
			a.wsConn.Close()
		}
		a.wsConn = ws

		for {
			messageType, p, err := ws.ReadMessage()
			if messageType == websocket.CloseMessage {
				return
			}

			if err != nil {
				log.Printf(`Error while reading from websocket: %v`, err)
				return
			}

			if messageType == websocket.TextMessage {
				for _, value := range a.providers {
					if bytes.HasPrefix(p, value.GetWorkerPrefix()) {
						value.WorkerHandler(bytes.TrimPrefix(p, value.GetWorkerPrefix()))
						break
					} else if bytes.HasPrefix(p, provider.ErrorPrefix) {
						log.Printf(`Error received from worker: %s`, bytes.TrimPrefix(p, provider.ErrorPrefix))
					}
				}
			}
		}
	})
}

// Handler create Handler with given App context
func (a *App) Handler() http.Handler {
	return a.authApp.HandlerWithFail(func(w http.ResponseWriter, r *http.Request, _ *auth.User) {
		a.RenderDashboard(w, r, http.StatusOK, nil)
	}, func(w http.ResponseWriter, r *http.Request, err error) {
		a.handleAuthFail(w, r, err)
	})
}

func (a *App) handleAuthFail(w http.ResponseWriter, r *http.Request, err error) {
	if auth.IsForbiddenErr(err) {
		httputils.Forbidden(w)
	} else if err == auth.ErrEmptyAuthorization {
		http.Redirect(w, r, path.Join(a.authApp.URL, `/redirect/github`), http.StatusFound)
	} else {
		httputils.Unauthorized(w, err)
	}
}
