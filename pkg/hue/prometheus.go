package hue

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

func (a *app) getMetrics(prefix, suffix string) prometheus.Gauge {
	name := fmt.Sprintf("%s_%s", prefix, suffix)
	if gauge, ok := a.prometheusCollectors[name]; ok {
		return gauge
	}

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "hue",
		Name:      name,
	})

	a.prometheusCollectors[name] = gauge
	a.prometheusRegisterer.MustRegister(gauge)

	return gauge
}

func (a *app) updatePrometheusSensors() {
	if a.prometheusRegisterer == nil {
		return
	}

	a.mutex.RLock()
	defer a.mutex.RUnlock()

	for _, sensor := range a.sensors {
		a.getMetrics(strings.ToLower(sensor.Name), "temperature").Set(float64(sensor.State.Temperature))
		a.getMetrics(strings.ToLower(sensor.Name), "battery").Set(float64(sensor.Config.Battery))
	}
}
