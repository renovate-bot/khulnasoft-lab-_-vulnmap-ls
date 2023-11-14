/*
 * © 2023 Khulnasoft Limited All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/khulnasoft-lab/go-application-framework/pkg/utils"
	"github.com/khulnasoft-lab/go-application-framework/pkg/workflow"

	"github.com/khulnasoft-lab/vulnmap-ls/application/entrypoint"

	"github.com/khulnasoft-lab/vulnmap-ls/ls_extension"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/khulnasoft-lab/vulnmap-ls/application/config"
	"github.com/khulnasoft-lab/vulnmap-ls/application/server"
)

func main() {
	defer entrypoint.OnPanicRecover()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	c := config.CurrentConfig()
	output, err := parseFlags(os.Args, c)
	if err != nil {
		fmt.Println(err, output)
		os.Exit(1)
	}
	if output != "" {
		entrypoint.PrintLicenseText(output)
	}

	log.Trace().Interface("environment", os.Environ()).Msg("start environment")
	server.Start(c)
	log.Info().Msg("Exiting...")
}

func parseFlags(args []string, c *config.Config) (string, error) {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	var buf bytes.Buffer
	flags.SetOutput(&buf)

	versionFlag := flags.Bool("v", false, "prints the version")
	logLevelFlag := flags.String("l", "info", "sets the log-level to <trace|debug|info|warn|error|fatal>")
	logPathFlag := flags.String("f", "", "sets the log file for the language server")
	formatFlag := flags.String(
		"o",
		config.FormatMd,
		"sets format of diagnostics. Accepted values \""+config.FormatMd+"\" and \""+config.FormatHtml+"\"")
	configFlag := flags.String(
		"c",
		"",
		"provide the full path of a config file to use. format VARIABLENAME=VARIABLEVALUE")
	reportErrorsFlag := flags.Bool(
		"reportErrors",
		false,
		"enables error reporting")

	licensesFlag := flags.Bool(
		"licenses",
		false,
		"displays license information")

	// remove extension command if specified to not fail flag parsing
	args = utils.RemoveSimilar(args, workflow.GetCommandFromWorkflowIdentifier(ls_extension.WORKFLOWID_LS))

	err := flags.Parse(args[1:])
	if err != nil {
		return buf.String(), err
	}

	if *versionFlag {
		return buf.String(), fmt.Errorf(config.Version)
	}

	if *licensesFlag {
		buf.Write([]byte(config.LicenseInformation))
	}

	c.SetConfigFile(*configFlag)
	c.Load()
	c.SetLogLevel(*logLevelFlag)
	c.SetLogPath(*logPathFlag)
	c.SetFormat(*formatFlag)
	if os.Getenv(config.SendErrorReportsKey) == "" {
		c.SetErrorReportingEnabled(*reportErrorsFlag)
	}

	config.SetCurrentConfig(c)
	return buf.String(), nil
}
