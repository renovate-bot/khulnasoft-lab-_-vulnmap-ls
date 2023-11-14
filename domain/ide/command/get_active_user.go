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

	noti "github.com/khulnasoft-lab/vulnmap-ls/domain/ide/notification"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
)

// oauthRefreshCommand is a command that refreshes the oauth token
// This is needed because the token is only valid for a certain period of time
// For doing this we call the whoami workflow that will refresh the token automatically
type getActiveUser struct {
	command     vulnmap.CommandData
	authService vulnmap.AuthenticationService
	notifier    noti.Notifier
}

func (cmd *getActiveUser) Command() vulnmap.CommandData {
	return cmd.command
}

func (cmd *getActiveUser) Execute(ctx context.Context) (any, error) {
	user, err := vulnmap.GetActiveUser()
	return user, err
}
