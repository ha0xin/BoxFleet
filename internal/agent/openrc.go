package agent

import (
	"bytes"
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
)

//go:embed templates/boxfleet-agent.openrc.tmpl
var agentOpenRCTemplate string

//go:embed templates/sing-box.openrc.tmpl
var singBoxOpenRCTemplate string

type openRCData struct {
	Command   string
	Arguments string
	Guard     string
	PIDFile   string
	OutputLog string
	ErrorLog  string
}

func renderOpenRCScript(name, raw string, data openRCData) (string, error) {
	tmpl, err := template.New(name).Funcs(template.FuncMap{"quote": strconv.Quote}).Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (a *Agent) InstallOpenRCScripts() error {
	if err := os.MkdirAll(filepath.Dir(a.Config.SingBoxConfig), 0o755); err != nil {
		return err
	}
	logDir := filepath.Join(a.Config.InstallDir, "log")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return err
	}
	singName := serviceName(InitSystemOpenRC, a.Config.SingBoxService)
	agentName := serviceName(InitSystemOpenRC, a.Config.AgentService)
	singScript, err := renderOpenRCScript("sing-box", singBoxOpenRCTemplate, openRCData{
		Command: a.Config.SingBoxPath, Arguments: "run -c " + strconv.Quote(a.Config.SingBoxConfig),
		PIDFile: filepath.Join("/run", singName+".pid"), OutputLog: filepath.Join(logDir, singName+".log"),
		ErrorLog: filepath.Join(logDir, singName+".log"),
	})
	if err != nil {
		return err
	}
	agentScript, err := renderOpenRCScript("boxfleet-agent", agentOpenRCTemplate, openRCData{
		Command: a.Config.AgentPath, Arguments: "run --config " + strconv.Quote(a.Config.AgentConfigPath),
		Guard:   strconv.Quote(a.Config.AgentGuardPath) + " guard --config " + strconv.Quote(a.Config.AgentConfigPath),
		PIDFile: filepath.Join("/run", agentName+".pid"), OutputLog: filepath.Join(logDir, agentName+".log"),
		ErrorLog: filepath.Join(logDir, agentName+".log"),
	})
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join("/etc/init.d", singName), []byte(singScript), 0o755); err != nil {
		return err
	}
	return atomicWrite(filepath.Join("/etc/init.d", agentName), []byte(agentScript), 0o755)
}
