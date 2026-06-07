package install

import (
	"bytes"
	_ "embed"
	"text/template"
)

const (
	DefaultRepo           = "ha0xin/BoxFleet"
	DefaultSingBoxVersion = "v1.13.13"
)

//go:embed install.sh.tmpl
var scriptTemplate string

type ScriptData struct {
	Repo            string
	BoxFleetVersion string
	SingBoxVersion  string
}

func Script(data ScriptData) ([]byte, error) {
	if data.Repo == "" {
		data.Repo = DefaultRepo
	}
	if data.BoxFleetVersion == "" {
		data.BoxFleetVersion = "dev"
	}
	if data.SingBoxVersion == "" {
		data.SingBoxVersion = DefaultSingBoxVersion
	}
	tmpl, err := template.New("install.sh").Parse(scriptTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
