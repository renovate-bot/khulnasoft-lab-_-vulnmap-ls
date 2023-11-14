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

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog/log"
	"github.com/khulnasoft-lab/go-application-framework/pkg/configuration"
	"github.com/khulnasoft-lab/go-application-framework/pkg/local_workflows/json_schemas"

	"github.com/khulnasoft-lab/vulnmap-ls/application/config"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/ide/converter"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/ide/hover"
	noti "github.com/khulnasoft-lab/vulnmap-ls/domain/ide/notification"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
	"github.com/khulnasoft-lab/vulnmap-ls/infrastructure/analytics"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/lsp"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/product"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/uri"
)

type FolderStatus int

const (
	Unscanned FolderStatus = iota
	Scanned   FolderStatus = iota
)

var (
	os = map[string]string{
		"darwin":  "macOS",
		"linux":   "Linux",
		"windows": "Windows",
	}

	arch = map[string]string{
		"amd64": "x86_64",
		"arm64": "arm64",
		"386":   "386",
	}
)

// TODO: 3: Extract reporting logic to a separate service

// Folder contains files that can be scanned,
// it orchestrates vulnmap scans and provides a caching layer to avoid unnecessary computing
type Folder struct {
	path                    string
	name                    string
	status                  FolderStatus
	documentDiagnosticCache *xsync.MapOf[string, []vulnmap.Issue]
	scanner                 vulnmap.Scanner
	hoverService            hover.Service
	mutex                   sync.Mutex
	scanNotifier            vulnmap.ScanNotifier
	notifier                noti.Notifier
}

func NewFolder(path string, name string, scanner vulnmap.Scanner, hoverService hover.Service, scanNotifier vulnmap.ScanNotifier, notifier noti.Notifier) *Folder {
	folder := Folder{
		scanner:      scanner,
		path:         strings.TrimSuffix(path, "/"),
		name:         name,
		status:       Unscanned,
		hoverService: hoverService,
		scanNotifier: scanNotifier,
		notifier:     notifier,
	}
	folder.documentDiagnosticCache = xsync.NewMapOf[string, []vulnmap.Issue]()
	return &folder
}

func (f *Folder) IsScanned() bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.status == Scanned
}

func (f *Folder) ClearScannedStatus() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.status = Unscanned
}

func (f *Folder) SetStatus(status FolderStatus) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.status = status
}

func (f *Folder) ScanFolder(ctx context.Context) {
	f.scan(ctx, f.path)
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.status = Scanned
}

func (f *Folder) ScanFile(ctx context.Context, path string) {
	f.scan(ctx, path)
}

func (f *Folder) Contains(path string) bool {
	return uri.FolderContains(f.path, path)
}

// ClearDiagnosticsFromFile will clear all diagnostics of a file from memory, and send a notification to the client
// with empty diagnostics results for the specific file
func (f *Folder) ClearDiagnosticsFromFile(filePath string) {
	// todo: can we manage the cache internally without leaking it, e.g. by using as a key an MD5 hash rather than a path and defining a TTL?
	f.documentDiagnosticCache.Delete(filePath)
	if scanner, ok := f.scanner.(vulnmap.InlineValueProvider); ok {
		scanner.ClearInlineValues(filePath)
	}
	f.notifier.Send(lsp.PublishDiagnosticsParams{
		URI:         uri.PathToUri(filePath),
		Diagnostics: []lsp.Diagnostic{},
	})
	f.ClearScannedStatus()

}

func (f *Folder) ClearDiagnosticsFromPathRecursively(removedPath string) {
	f.documentDiagnosticCache.Range(func(key string, value []vulnmap.Issue) bool {
		if strings.Contains(key, removedPath) {
			f.ClearDiagnosticsFromFile(key)
		}

		return true // Continue the iteration
	})
}

func (f *Folder) scan(ctx context.Context, path string) {
	const method = "domain.ide.workspace.folder.scan"
	if !f.IsTrusted() {
		log.Warn().Str("path", path).Str("method", method).Msg("skipping scan of untrusted path")
		return
	}
	issuesSlice := f.DocumentDiagnosticsFromCache(path)
	if issuesSlice != nil {
		log.Info().Str("method", method).
			Int("issueSliceLength", len(issuesSlice)).
			Msgf("Cached results found: Skipping scan for %s", path)
		f.processResults(vulnmap.ScanData{
			Issues: issuesSlice,
		})
		return
	}

	f.scanner.Scan(ctx, path, f.processResults, f.path)
}

func (f *Folder) DocumentDiagnosticsFromCache(file string) []vulnmap.Issue {
	issues, _ := f.documentDiagnosticCache.Load(file)
	if issues == nil {
		return nil
	}
	return issues
}

func (f *Folder) processResults(scanData vulnmap.ScanData) {
	if scanData.Err != nil {
		f.scanNotifier.SendError(scanData.Product, f.path)
		log.Err(scanData.Err).
			Str("method", "processResults").
			Str("product", string(scanData.Product)).
			Msg("Product returned an error")
		return
	}

	dedupMap := f.createDedupMap()

	// TODO: perform issue diffing (current <-> newly reported)
	// Update diagnostic cache
	for _, issue := range scanData.Issues {
		cachedIssues, _ := f.documentDiagnosticCache.Load(issue.AffectedFilePath)
		if cachedIssues == nil {
			cachedIssues = []vulnmap.Issue{}
		}

		if !dedupMap[f.getUniqueIssueID(issue)] {
			cachedIssues = append(cachedIssues, issue)
			incrementSeverityCount(&scanData, issue)
		}

		f.documentDiagnosticCache.Store(issue.AffectedFilePath, cachedIssues)

	}
	log.Debug().Str("method", "processResults").Interface("scanData", scanData).Msg("Finished processing results. Sending analytics.")
	sendAnalytics(&scanData)

	// Filter and publish cached diagnostics
	f.FilterAndPublishCachedDiagnostics(scanData.Product)
}

func incrementSeverityCount(scanData *vulnmap.ScanData, issue vulnmap.Issue) {
	issueProduct := issue.Product
	if issueProduct == "" {
		log.Debug().Str("method", "incrementSeverityCount").Msg("Issue product is empty. Setting to unknown")
		issueProduct = "unknown"
	}

	initializeSeverityCountForProduct(scanData, issueProduct)

	severityCount, exists := scanData.SeverityCount[issueProduct]
	if !exists {
		severityCount = vulnmap.SeverityCount{}
	}

	switch issue.Severity {
	case vulnmap.Critical:
		severityCount.Critical++
	case vulnmap.High:
		severityCount.High++
	case vulnmap.Medium:
		severityCount.Medium++
	case vulnmap.Low:
		severityCount.Low++
	}

	scanData.SeverityCount[issueProduct] = severityCount // reassign the value to the map
}

func initializeSeverityCountForProduct(scanData *vulnmap.ScanData, productType product.Product) {
	if scanData.SeverityCount == nil {
		scanData.SeverityCount = make(map[product.Product]vulnmap.SeverityCount)
	}

	if productType == "" {
		log.Debug().Str("method", "initializeSeverityCountForProduct").Msg("Product is empty. Setting to unknown")
		productType = "unknown"
	}

	if _, exists := scanData.SeverityCount[productType]; !exists {
		scanData.SeverityCount[productType] = vulnmap.SeverityCount{}
	}
}

func sendAnalytics(data *vulnmap.ScanData) {
	initializeSeverityCountForProduct(data, data.Product)

	c := config.CurrentConfig()
	gafConfig := c.Engine().GetConfiguration()

	logger := c.Logger().With().Str("method", "folder.sendAnalytics").Logger()
	if data.Product == "" {
		logger.Debug().Any("data", data).Msg("Skipping analytics for empty product")
		return
	}

	if data.Err != nil {
		logger.Debug().Err(data.Err).Msg("Skipping analytics for error")
		return
	}

	scanEvent := json_schemas.ScanDoneEvent{}
	// Populate the fields with data
	scanEvent.Data.Type = "analytics"
	scanEvent.Data.Attributes.DeviceId = c.DeviceID()
	scanEvent.Data.Attributes.Application = "vulnmap-ls"
	scanEvent.Data.Attributes.ApplicationVersion = config.Version
	scanEvent.Data.Attributes.Os = os[runtime.GOOS]
	scanEvent.Data.Attributes.Arch = arch[runtime.GOARCH]
	scanEvent.Data.Attributes.IntegrationName = gafConfig.GetString(configuration.INTEGRATION_NAME)
	scanEvent.Data.Attributes.IntegrationVersion = gafConfig.GetString(configuration.INTEGRATION_VERSION)
	scanEvent.Data.Attributes.IntegrationEnvironment = gafConfig.GetString(configuration.INTEGRATION_ENVIRONMENT)
	scanEvent.Data.Attributes.IntegrationEnvironmentVersion = gafConfig.GetString(configuration.INTEGRATION_ENVIRONMENT_VERSION)
	scanEvent.Data.Attributes.EventType = "Scan done"
	scanEvent.Data.Attributes.Status = "Success"
	scanEvent.Data.Attributes.ScanType = string(data.Product)
	scanEvent.Data.Attributes.UniqueIssueCount.Critical = data.SeverityCount[data.Product].Critical
	scanEvent.Data.Attributes.UniqueIssueCount.High = data.SeverityCount[data.Product].High
	scanEvent.Data.Attributes.UniqueIssueCount.Medium = data.SeverityCount[data.Product].Medium
	scanEvent.Data.Attributes.UniqueIssueCount.Low = data.SeverityCount[data.Product].Low
	scanEvent.Data.Attributes.DurationMs = fmt.Sprintf("%d", data.DurationMs)
	scanEvent.Data.Attributes.TimestampFinished = data.TimestampFinished

	bytes, err := json.Marshal(scanEvent)
	if err != nil {
		logger.Err(err).Msg("Error marshalling scan event")
		return
	}

	err = analytics.SendAnalyticsToAPI(c, bytes)
	if err != nil {
		logger.Err(err).Msg("Error sending analytics to API")
		return
	}
}

func (f *Folder) FilterAndPublishCachedDiagnostics(product product.Product) {
	issuesByFile := f.filterCachedDiagnostics()
	f.publishDiagnostics(product, issuesByFile)
}

func (f *Folder) filterCachedDiagnostics() (fileIssues map[string][]vulnmap.Issue) {
	logger := log.With().Str("method", "filterCachedDiagnostics").Logger()

	var issuesByFile = map[string][]vulnmap.Issue{}
	if f.documentDiagnosticCache.Size() == 0 {
		return issuesByFile
	}

	filterSeverity := config.CurrentConfig().FilterSeverity()
	logger.Debug().Interface("filterSeverity", filterSeverity).Msg("Filtering issues by severity")

	supportedIssueTypes := config.CurrentConfig().DisplayableIssueTypes()
	f.documentDiagnosticCache.Range(func(filePath string, issues []vulnmap.Issue) bool {
		// Consider doing the loop body in parallel for performance (and use a thread-safe map)
		filteredIssues := FilterIssues(issues, supportedIssueTypes)
		issuesByFile[filePath] = filteredIssues
		return true
	})

	return issuesByFile
}

func FilterIssues(issues []vulnmap.Issue, supportedIssueTypes map[product.FilterableIssueType]bool) []vulnmap.Issue {
	logger := log.With().Str("method", "FilterIssues").Logger()
	filteredIssues := make([]vulnmap.Issue, 0)

	for _, issue := range issues {
		// Logging here might hurt performance, should benchmark if filtering is slow
		if isVisibleSeverity(issue) && supportedIssueTypes[issue.GetFilterableIssueType()] {
			logger.Trace().Msgf("Including visible severity issue: %v", issue)
			filteredIssues = append(filteredIssues, issue)
		} else {
			logger.Trace().Msgf("Filtering out issue %v", issue)
		}
	}
	return filteredIssues
}

func isVisibleSeverity(issue vulnmap.Issue) bool {
	switch issue.Severity {
	case vulnmap.Critical:
		return config.CurrentConfig().FilterSeverity().Critical
	case vulnmap.High:
		return config.CurrentConfig().FilterSeverity().High
	case vulnmap.Medium:
		return config.CurrentConfig().FilterSeverity().Medium
	case vulnmap.Low:
		return config.CurrentConfig().FilterSeverity().Low
	}
	return false
}

func (f *Folder) publishDiagnostics(product product.Product, issuesByFile map[string][]vulnmap.Issue) {
	f.sendDiagnostics(issuesByFile)
	f.sendScanResults(product, issuesByFile)
	f.sendHovers(issuesByFile) // TODO: this locks up the thread, need to investigate
}

func (f *Folder) createDedupMap() (dedupMap map[string]bool) {
	dedupMap = make(map[string]bool)
	f.documentDiagnosticCache.Range(func(key string, value []vulnmap.Issue) bool {
		issues := value
		for _, issue := range issues {
			uniqueID := f.getUniqueIssueID(issue)
			dedupMap[uniqueID] = true
		}
		return true
	})
	return dedupMap
}

func (f *Folder) getUniqueIssueID(issue vulnmap.Issue) string {
	uniqueID := issue.ID + "|" + issue.AffectedFilePath
	return uniqueID
}

func (f *Folder) sendDiagnostics(issuesByFile map[string][]vulnmap.Issue) {
	for path, issues := range issuesByFile {
		f.sendDiagnosticsForFile(path, issues)
	}
}

func (f *Folder) sendDiagnosticsForFile(path string, issues []vulnmap.Issue) {
	log.Debug().Str("method", "sendDiagnosticsForFile").Str("affectedFilePath", path).Int("issueCount",
		len(issues)).Send()
	f.notifier.Send(lsp.PublishDiagnosticsParams{
		URI:         uri.PathToUri(path),
		Diagnostics: converter.ToDiagnostics(issues),
	})
}

func (f *Folder) sendHovers(issuesByFile map[string][]vulnmap.Issue) {
	for path, issues := range issuesByFile {
		f.sendHoversForFile(path, issues)
	}
}

func (f *Folder) sendHoversForFile(path string, issues []vulnmap.Issue) {
	f.hoverService.Channel() <- converter.ToHoversDocument(path, issues)
}

func (f *Folder) Path() string         { return f.path }
func (f *Folder) Name() string         { return f.name }
func (f *Folder) Status() FolderStatus { return f.status }

func (f *Folder) IssuesFor(filePath string, requestedRange vulnmap.Range) (matchingIssues []vulnmap.Issue) {
	method := "domain.ide.workspace.folder.getCodeActions"
	issues := f.DocumentDiagnosticsFromCache(filePath)
	for _, issue := range issues {
		if issue.Range.Overlaps(requestedRange) {
			log.Debug().Str("method", method).Msg("appending code action for issue " + issue.String())
			matchingIssues = append(matchingIssues, issue)
		}
	}

	log.Debug().Str("method", method).Msgf(
		"found %d code actions for %s, %s",
		len(matchingIssues),
		filePath,
		requestedRange,
	)
	return matchingIssues
}

func (f *Folder) AllIssuesFor(filePath string) (matchingIssues []vulnmap.Issue) {
	return f.DocumentDiagnosticsFromCache(filePath)
}

func (f *Folder) ClearDiagnostics() {
	f.documentDiagnosticCache.Range(func(key string, _ []vulnmap.Issue) bool {
		// we must republish empty diagnostics for all files that were reported with diagnostics
		f.notifier.Send(lsp.PublishDiagnosticsParams{
			URI:         uri.PathToUri(key),
			Diagnostics: []lsp.Diagnostic{},
		})
		f.documentDiagnosticCache.Delete(key)
		return true
	})
}

func (f *Folder) ClearDiagnosticsByIssueType(removedType product.FilterableIssueType) {
	f.documentDiagnosticCache.Range(func(filePath string, previousIssues []vulnmap.Issue) bool {
		newIssues := []vulnmap.Issue{}
		for _, issue := range previousIssues {
			if issue.GetFilterableIssueType() != removedType {
				newIssues = append(newIssues, issue)
			}
		}

		if len(previousIssues) != len(newIssues) { // Only send diagnostics update when issues were removed
			f.documentDiagnosticCache.Store(filePath, newIssues)
			f.sendDiagnosticsForFile(filePath, newIssues)
			f.sendHoversForFile(filePath, newIssues)
		}

		return true
	})
}

func (f *Folder) IsTrusted() bool {
	if !config.CurrentConfig().IsTrustedFolderFeatureEnabled() {
		return true
	}

	for _, path := range config.CurrentConfig().TrustedFolders() {
		if strings.HasPrefix(f.path, path) {
			return true
		}
	}
	return false
}

func (f *Folder) sendScanResults(processedProduct product.Product, issuesByFile map[string][]vulnmap.Issue) {
	var productIssues []vulnmap.Issue
	for _, issues := range issuesByFile {
		productIssues = append(productIssues, issues...)
	}

	if processedProduct != "" {
		f.scanNotifier.SendSuccess(processedProduct, f.Path(), productIssues)
	} else {
		f.scanNotifier.SendSuccessForAllProducts(f.Path(), productIssues)
	}
}
