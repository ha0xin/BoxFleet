package bf

import (
	"bytes"
	"database/sql"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func renderTable(cmd *cobra.Command, header table.Row, rows []table.Row) {
	t := table.NewWriter()
	t.SetOutputMirror(cmd.OutOrStdout())
	t.SetStyle(table.StyleDefault)
	t.AppendHeader(header)
	t.AppendRows(rows)
	t.Render()
}

func runCommand(cmd *cobra.Command, name string, args ...string) error {
	c := exec.CommandContext(cmd.Context(), name, args...)
	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	if output.Len() > 0 {
		fmt.Fprint(cmd.ErrOrStderr(), output.String())
	}
	return nil
}

func shellQuote(value string) string {
	return strconv.Quote(value)
}

func formatNullString(value sql.NullString) string {
	if !value.Valid || value.String == "" {
		return "-"
	}
	return value.String
}

func formatNullInt(value sql.NullInt64) string {
	if !value.Valid {
		return "-"
	}
	return strconv.FormatInt(value.Int64, 10)
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func ss2022KeyLength(method string) (int, error) {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16, nil
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32, nil
	default:
		return 0, fmt.Errorf("unsupported Shadowsocks 2022 method %q", method)
	}
}

func titleASCII(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
