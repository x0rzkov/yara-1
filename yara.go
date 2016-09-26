package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/crackcomm/go-clitable"
	"github.com/fatih/structs"
	"github.com/hillu/go-yara"
	"github.com/maliceio/go-plugin-utils/database/elasticsearch"
	"github.com/maliceio/go-plugin-utils/utils"
	"github.com/urfave/cli"
)

// Version stores the plugin's version
var Version string

// BuildTime stores the plugin's build time
var BuildTime string

const (
	name     = "yara"
	category = "av"
)

type pluginResults struct {
	ID   string      `json:"id" gorethink:"id,omitempty"`
	Data ResultsData `json:"yara" gorethink:"yara"`
}

// Yara json object
type Yara struct {
	Results ResultsData `json:"yara"`
}

// ResultsData json object
type ResultsData struct {
	Matches []yara.MatchRule `json:"matches" gorethink:"matches"`
}

// TODO: handle more than just the first Offset, handle multiple MatchStrings
func printMarkDownTable(yara Yara) {
	fmt.Println("#### Yara")
	if yara.Results.Matches != nil {
		table := clitable.New([]string{"Rule", "Description", "Offset", "Data", "Tags"})
		for _, match := range yara.Results.Matches {
			var tags string
			if len(match.Tags) == 0 {
				tags = ""
			} else {
				tags = match.Tags[0]
			}
			table.AddRow(map[string]interface{}{
				"Rule":        match.Rule,
				"Description": match.Meta["description"],
				"Offset":      match.Strings[0].Offset,
				"Data":        string(match.Strings[0].Data),
				"Tags":        tags,
			})
		}
		table.Markdown = true
		table.Print()
	} else {
		fmt.Println(" - No Matches")
	}
}

// scanFile scans file with all yara rules in the rules folder
func scanFile(path string, rulesDir string) ResultsData {
	yaraResults := ResultsData{}

	fileList := []string{}
	err := filepath.Walk(rulesDir, func(path string, f os.FileInfo, err error) error {
		fileList = append(fileList, path)
		return nil
	})
	assert(err)

	comp, err := yara.NewCompiler()
	assert(err)

	for _, file := range fileList {
		// fmt.Println(file)
		f, err := os.Open(file)
		assert(err)
		comp.AddFile(f, "malice")
		f.Close()
	}

	r, err := comp.GetRules()

	// args: filename string, flags ScanFlags, timeout time.Duration
	matches, err := r.ScanFile(path, 0, 60)
	assert(err)
	yaraResults.Matches = matches
	// fmt.Printf("Matches: %+v", matches)
	return yaraResults
}

var appHelpTemplate = `Usage: {{.Name}} {{if .Flags}}[OPTIONS] {{end}}COMMAND [arg...]

{{.Usage}}

Version: {{.Version}}{{if or .Author .Email}}

Author:{{if .Author}}
  {{.Author}}{{if .Email}} - <{{.Email}}>{{end}}{{else}}
  {{.Email}}{{end}}{{end}}
{{if .Flags}}
Options:
  {{range .Flags}}{{.}}
  {{end}}{{end}}
Commands:
  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Run '{{.Name}} COMMAND --help' for more information on a command.
`

func main() {
	cli.AppHelpTemplate = appHelpTemplate
	app := cli.NewApp()
	app.Name = "yara"
	app.Author = "blacktop"
	app.Email = "https://github.com/blacktop"
	app.Version = Version + ", BuildTime: " + BuildTime
	app.Compiled, _ = time.Parse("20060102", BuildTime)
	app.Usage = "Malice YARA Plugin"
	var rules string
	var table bool
	var elasitcsearch string
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose, V",
			Usage: "verbose output",
		},
		cli.StringFlag{
			Name:        "elasitcsearch",
			Value:       "",
			Usage:       "elasitcsearch address for Malice to store results",
			EnvVar:      "MALICE_ELASTICSEARCH",
			Destination: &elasitcsearch,
		},
		cli.BoolFlag{
			Name:   "post, p",
			Usage:  "POST results to Malice webhook",
			EnvVar: "MALICE_ENDPOINT",
		},
		cli.BoolFlag{
			Name:   "proxy, x",
			Usage:  "proxy settings for Malice webhook endpoint",
			EnvVar: "MALICE_PROXY",
		},
		cli.BoolFlag{
			Name:        "table, t",
			Usage:       "output as Markdown table",
			Destination: &table,
		},
		cli.StringFlag{
			Name:        "rules",
			Value:       "/rules",
			Usage:       "YARA rules directory",
			Destination: &rules,
		},
	}
	app.ArgsUsage = "FILE to scan with YARA"
	app.Action = func(c *cli.Context) error {
		if c.Args().Present() {
			path := c.Args().First()
			// Check that file exists
			if _, err := os.Stat(path); os.IsNotExist(err) {
				assert(err)
			}

			if c.Bool("verbose") {
				log.SetLevel(log.DebugLevel)
			}

			yara := Yara{Results: scanFile(path, rules)}

			// upsert into Database
			elasticsearch.InitElasticSearch()
			elasticsearch.WritePluginResultsToDatabase(elasticsearch.PluginResults{
				ID:       utils.Getopt("MALICE_SCANID", utils.GetSHA256(path)),
				Name:     name,
				Category: category,
				Data:     structs.Map(yara.Results),
			})

			if table {
				printMarkDownTable(yara)
			} else {
				yaraJSON, err := json.Marshal(yara)
				assert(err)
				fmt.Println(string(yaraJSON))
			}
		} else {
			log.Fatal(fmt.Errorf("Please supply a file to scan with YARA"))
		}
		return nil
	}

	err := app.Run(os.Args)
	assert(err)
}
