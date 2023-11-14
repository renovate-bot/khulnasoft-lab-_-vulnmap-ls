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

package oauth

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/khulnasoft-lab/go-application-framework/pkg/auth"
	"github.com/khulnasoft-lab/go-application-framework/pkg/configuration"

	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
)

type oAuthProvider struct {
	authenticator auth.Authenticator
	config        configuration.Configuration
	authURL       string
}

func (p *oAuthProvider) GetCheckAuthenticationFunction() vulnmap.AuthenticationFunction {
	return vulnmap.AuthenticationCheck
}

func NewOAuthProvider(config configuration.Configuration, authenticator auth.Authenticator) vulnmap.AuthenticationProvider {
	log.Debug().Msg("creating new OAuth provider")
	return &oAuthProvider{authenticator: authenticator, config: config}
}

func (p *oAuthProvider) Authenticate(_ context.Context) (string, error) {
	err := p.authenticator.Authenticate()
	log.Debug().Msg("authenticated with OAuth")
	return p.config.GetString(auth.CONFIG_KEY_OAUTH_TOKEN), err
}

func (p *oAuthProvider) SetAuthURL(url string) {
	p.authURL = url
}

func (p *oAuthProvider) ClearAuthentication(_ context.Context) error {
	log.Debug().Msg("clearing authentication")
	p.config.Set(auth.CONFIG_KEY_OAUTH_TOKEN, "")
	p.config.Set(configuration.AUTHENTICATION_TOKEN, "")
	p.config.Set(configuration.AUTHENTICATION_BEARER_TOKEN, "")
	return nil
}

func (p *oAuthProvider) AuthURL(_ context.Context) string {
	return p.authURL
}

func (p *oAuthProvider) Authenticator() auth.Authenticator {
	return p.authenticator
}
