package cmd

import (
	"bytes"
	"fmt"
	"html/template"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	RootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Long:  `Display version and build information about vault-kv-search.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(buildInfo())
	},
}

var (
	Version   = "unknown"
	Revision  = "unknown"
	Branch    = "unknown"
	BuildUser = "unknown"
	BuildDate = "unknown"
	GoVersion = runtime.Version()
	GoOS      = runtime.GOOS
	GoArch    = runtime.GOARCH
	Platform  = GoOS + "/" + GoArch
)

var versionInfoTmpl = `
version:    {{.version}}
  branch:   {{.branch}}
  revision: {{.revision}})
build user: {{.buildUser}}
build date: {{.buildDate}}
go version: {{.goVersion}}
platform:   {{.platform}}
`

func buildInfo() string {
	m := map[string]string{
		"version":   Version,
		"revision":  Revision,
		"branch":    Branch,
		"buildUser": BuildUser,
		"buildDate": BuildDate,
		"goVersion": GoVersion,
		"platform":  GoOS + "/" + GoArch,
	}
	t := template.Must(template.New("version").Parse(versionInfoTmpl))

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "version", m); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}
