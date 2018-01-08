package hue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ViBiOh/httputils"
)

type rule struct {
	ID         string           `json:"-"`
	Status     string           `json:"status,omitempty"`
	Name       string           `json:"name,omitempty"`
	Actions    []*ruleAction    `json:"actions,omitempty"`
	Conditions []*ruleCondition `json:"conditions,omitempty"`
}

type ruleAction struct {
	Address string                 `json:"address,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
	Method  string                 `json:"method,omitempty"`
}

type ruleCondition struct {
	Address  string `json:"address,omitempty"`
	Operator string `json:"operator,omitempty"`
	Value    string `json:"value,omitempty"`
}

func (a *App) createRule(r *rule) error {
	content, err := httputils.RequestJSON(a.bridgeURL+`/rules`, r, nil, http.MethodPost)
	if err != nil {
		return fmt.Errorf(`Error while creating rule: %v`, err)
	}
	if !bytes.Contains(content, []byte(`success`)) {
		return fmt.Errorf(`Error while creating rule: %s`, content)
	}

	var response []map[string]map[string]string
	if err := json.Unmarshal(content, &response); err != nil {
		return fmt.Errorf(`Error while unmarshalling create rule response: %s`, err)
	}

	r.ID = response[0][`success`][`id`]

	return nil
}

func (a *App) updateRule(r *rule) error {
	content, err := httputils.RequestJSON(a.bridgeURL+`/rules/`+r.ID, r, nil, http.MethodPut)
	if err != nil {
		return fmt.Errorf(`Error while updating rule: %v`, err)
	}
	if !bytes.Contains(content, []byte(`success`)) {
		return fmt.Errorf(`Error while updating rule: %s`, content)
	}

	return nil
}

func (a *App) deleteRule(id string) error {
	content, err := httputils.Request(a.bridgeURL+`/rules/`+id, nil, nil, http.MethodDelete)
	if err != nil {
		return fmt.Errorf(`Error while deleting rule: %v`, err)
	}
	if !bytes.Contains(content, []byte(`success`)) {
		return fmt.Errorf(`Error while deleting rule: %s`, content)
	}

	return nil
}

func (a *App) cleanRules() error {
	rules, err := a.listRulesOfSensor(a.tap.ID)
	if err != nil {
		return fmt.Errorf(`Error while listing rules: %v`, err)
	}

	for key := range rules {
		if err := a.deleteRule(key); err != nil {
			return fmt.Errorf(`Error while deleting rule: %v`, err)
		}
	}

	return nil
}