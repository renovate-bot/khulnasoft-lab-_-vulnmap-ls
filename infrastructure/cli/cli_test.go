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

package cli

import (
	"context"
	"testing"

	"github.com/adrg/xdg"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/khulnasoft-lab/vulnmap-ls/application/config"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/testutil"
)

func Test_ExpandParametersFromConfig(t *testing.T) {
	testutil.UnitTest(t)
	testOrg, err := uuid.NewUUID()
	assert.NoError(t, err)
	config.CurrentConfig().SetOrganization(testOrg.String())
	settings := config.CliSettings{
		Insecure: true,
	}
	config.CurrentConfig().SetCliSettings(&settings)
	var cmd = []string{"a", "b"}

	cmd = VulnmapCli{}.ExpandParametersFromConfig(cmd)

	assert.Contains(t, cmd, "a")
	assert.Contains(t, cmd, "b")
	assert.Contains(t, cmd, "--insecure")
	assert.Contains(t, cmd, "--org="+testOrg.String())
}

func TestGetCommand_AddsToEnvironmentAndSetsDir(t *testing.T) {
	testutil.UnitTest(t)
	config.CurrentConfig().SetTelemetryEnabled(false)

	cmd := VulnmapCli{}.getCommand([]string{"executable", "arg"}, xdg.DataHome, context.Background())

	assert.Equal(t, xdg.DataHome, cmd.Dir)
	assert.Contains(t, cmd.Env, DisableAnalyticsEnvVar+"=1")
}
