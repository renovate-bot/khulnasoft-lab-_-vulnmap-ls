/*
 * © 2023 Khulnasoft Limited
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

package command

import (
	"context"

	"github.com/rs/zerolog/log"

	noti "github.com/khulnasoft-lab/vulnmap-ls/domain/ide/notification"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/util"
)

type loginCommand struct {
	command     vulnmap.CommandData
	authService vulnmap.AuthenticationService
	notifier    noti.Notifier
}

func (cmd *loginCommand) Command() vulnmap.CommandData {
	return cmd.command
}

func (cmd *loginCommand) Execute(ctx context.Context) (any, error) {
	log.Debug().Str("method", "loginCommand.Execute").Msgf("logging in")
	token, err := cmd.authService.Authenticate(ctx)
	if err != nil {
		log.Err(err).Msg("Error on vulnmap.login command")
		cmd.notifier.SendError(err)
	}
	if err == nil && token != "" {
		log.Debug().Str("method", "loginCommand.Execute").
			Str("hashed token", util.Hash([]byte(token))[0:16]).
			Msgf("authentication successful, received token")
	}
	return nil, err
}
