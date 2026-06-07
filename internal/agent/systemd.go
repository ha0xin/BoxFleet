package agent

import (
	"bytes"
	_ "embed"
	"strconv"
	"text/template"
)

//go:embed templates/sing-box.service.tmpl
var singBoxUnitTemplate string

//go:embed templates/boxfleet-agent.service.tmpl
var agentUnitTemplate string

type systemdUnitData struct {
	SingBoxPath       string
	SingBoxConfig     string
	AgentPath         string
	AgentConfigPath   string
	Restart           string
	RestartSec        string
	SingBoxLimitFiles int
}

func renderSystemdUnit(name, raw string, data systemdUnitData) (string, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"quote": strconv.Quote,
	}).Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}
