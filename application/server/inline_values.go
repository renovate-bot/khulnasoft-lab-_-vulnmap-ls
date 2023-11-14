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

package server

import (
	"context"

	"github.com/creachadair/jrpc2"
	"github.com/creachadair/jrpc2/handler"

	"github.com/khulnasoft-lab/vulnmap-ls/application/config"
	"github.com/khulnasoft-lab/vulnmap-ls/application/di"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/ide/converter"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/lsp"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/uri"
)

func textDocumentInlineValueHandler(c *config.Config) jrpc2.Handler {
	return handler.New(func(ctx context.Context, params lsp.InlineValueParams) (any, error) {
		logger := c.Logger().With().Str("method", "textDocumentInlineValueHandler").Logger()
		documentURI := params.TextDocument.URI
		logger.Info().Msgf("Request for %s:%s RECEIVED", documentURI, params.Range.String())
		defer logger.Info().Msgf("Request for %s:%s DONE", documentURI, params.Range.String())
		if s, ok := di.Scanner().(vulnmap.InlineValueProvider); ok {
			filePath := uri.PathFromUri(documentURI)
			values, err := s.GetInlineValues(filePath, converter.FromRange(params.Range))
			if err != nil {
				return nil, err
			}
			lspInlineValues := converter.ToInlineValues(values)
			logger.Debug().Msgf("found %d inline values for %s", len(values), filePath)
			return lspInlineValues, nil
		}
		return nil, nil
	})
}
