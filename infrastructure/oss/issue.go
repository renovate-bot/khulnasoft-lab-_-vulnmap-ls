/*
 * © 2022-2023 Khulnasoft Limited
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

package oss

import (
	_ "embed"
	"fmt"
	"net/url"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/khulnasoft-lab/vulnmap-ls/application/config"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/observability/error_reporting"
	"github.com/khulnasoft-lab/vulnmap-ls/domain/vulnmap"
	"github.com/khulnasoft-lab/vulnmap-ls/infrastructure/learn"
	"github.com/khulnasoft-lab/vulnmap-ls/internal/product"
)

var issuesSeverity = map[string]vulnmap.Severity{
	"critical": vulnmap.Critical,
	"high":     vulnmap.High,
	"low":      vulnmap.Low,
	"medium":   vulnmap.Medium,
}

func (i *ossIssue) AddCodeActions(learnService learn.Service, ep error_reporting.ErrorReporter) (actions []vulnmap.
	CodeAction) {
	title := fmt.Sprintf("Open description of '%s affecting package %s' in browser (Vulnmap)", i.Title, i.PackageName)
	command := &vulnmap.CommandData{
		Title:     title,
		CommandId: vulnmap.OpenBrowserCommand,
		Arguments: []any{i.CreateIssueURL().String()},
	}

	action, _ := vulnmap.NewCodeAction(title, nil, command)
	actions = append(actions, action)

	codeAction := i.AddVulnmapLearnAction(learnService, ep)
	if codeAction != nil {
		actions = append(actions, *codeAction)
	}
	return actions
}

func (i *ossIssue) AddVulnmapLearnAction(learnService learn.Service, ep error_reporting.ErrorReporter) (action *vulnmap.
	CodeAction) {
	if config.CurrentConfig().IsVulnmapLearnCodeActionsEnabled() {
		lesson, err := learnService.GetLesson(i.PackageManager, i.Id, i.Identifiers.CWE, i.Identifiers.CVE, vulnmap.DependencyVulnerability)
		if err != nil {
			msg := "failed to get lesson"
			log.Err(err).Msg(msg)
			ep.CaptureError(errors.WithMessage(err, msg))
			return nil
		}

		if lesson != nil && lesson.Url != "" {
			title := fmt.Sprintf("Learn more about %s (Vulnmap)", i.Title)
			action = &vulnmap.CodeAction{
				Title: title,
				Command: &vulnmap.CommandData{
					Title:     title,
					CommandId: vulnmap.OpenBrowserCommand,
					Arguments: []any{lesson.Url},
				},
			}
			i.lesson = lesson
			log.Debug().Str("method", "oss.issue.AddVulnmapLearnAction").Msgf("Learn action: %v", action)
		}
	}
	return action
}

func (i *ossIssue) GetExtendedMessage(issue ossIssue) string {
	title := issue.Title
	description := issue.Description

	if config.CurrentConfig().Format() == config.FormatHtml {
		title = string(markdown.ToHTML([]byte(title), nil, nil))
		description = string(markdown.ToHTML([]byte(description), nil, nil))
	}
	summary := fmt.Sprintf("### Vulnerability %s %s %s \n **Fixed in: %s | Exploit maturity: %s**",
		issue.createCveLink(),
		issue.createCweLink(),
		issue.createIssueUrlMarkdown(),
		issue.createFixedIn(),
		strings.ToUpper(issue.Severity),
	)

	return fmt.Sprintf("\n### %s: %s affecting %s package \n%s \n%s",
		issue.Id,
		title,
		issue.PackageName,
		summary,
		description)
}

func (i *ossIssue) createCveLink() string {
	var formattedCve string
	for _, c := range i.Identifiers.CVE {
		formattedCve += fmt.Sprintf("| [%s](https://cve.mitre.org/cgi-bin/cvename.cgi?name=%s)", c, c)
	}
	return formattedCve
}

func (i *ossIssue) createIssueUrlMarkdown() string {
	return fmt.Sprintf("| [%s](%s)", i.Id, i.CreateIssueURL().String())
}

func (i *ossIssue) CreateIssueURL() *url.URL {
	parse, err := url.Parse("https://vulnmap.khulnasoft.com/vuln/" + i.Id)
	if err != nil {
		log.Err(err).Msg("Unable to create issue link for issue:" + i.Id)
	}
	return parse
}

func (i *ossIssue) createFixedIn() string {
	var f string
	if len(i.FixedIn) < 1 {
		f += "Not Fixed"
	} else {
		f += "@" + i.FixedIn[0]
		for _, version := range i.FixedIn[1:] {
			f += fmt.Sprintf(", %s", version)
		}
	}
	return f
}

func (i *ossIssue) createCweLink() string {
	var formattedCwe string
	for _, c := range i.Identifiers.CWE {
		id := strings.Replace(c, "CWE-", "", -1)
		formattedCwe += fmt.Sprintf("| [%s](https://cwe.mitre.org/data/definitions/%s.html)", c, id)
	}
	return formattedCwe
}

func (i *ossIssue) ToIssueSeverity() vulnmap.Severity {
	sev, ok := issuesSeverity[i.Severity]
	if !ok {
		return vulnmap.Low
	}
	return sev
}

func toIssue(
	affectedFilePath string,
	issue ossIssue,
	scanResult *scanResult,
	issueRange vulnmap.Range,
	learnService learn.Service,
	ep error_reporting.ErrorReporter,
) vulnmap.Issue {
	title := issue.Title

	if config.CurrentConfig().Format() == config.FormatHtml {
		title = string(markdown.ToHTML([]byte(title), nil, nil))
	}
	var action = "No fix available."
	var resolution = ""
	if issue.IsUpgradable {
		action = "Upgrade to:"
		resolution = issue.UpgradePath[len(issue.UpgradePath)-1].(string)
	} else {
		if len(issue.FixedIn) > 0 {
			action = "No direct upgrade path, fixed in:"
			resolution = fmt.Sprintf("%s@%s", issue.PackageName, issue.FixedIn[0])
		}
	}

	// find all issues with the same id
	matchingIssues := []ossIssue{}
	for _, otherIssue := range scanResult.Vulnerabilities {
		if otherIssue.Id == issue.Id {
			matchingIssues = append(matchingIssues, otherIssue)
		}
	}
	issue.matchingIssues = matchingIssues

	message := fmt.Sprintf(
		"%s affecting package %s. %s %s (Vulnmap)",
		title,
		issue.PackageName,
		action,
		resolution,
	)
	return vulnmap.Issue{
		ID:                  issue.Id,
		Message:             message,
		FormattedMessage:    issue.GetExtendedMessage(issue),
		Range:               issueRange,
		Severity:            issue.ToIssueSeverity(),
		AffectedFilePath:    affectedFilePath,
		Product:             product.ProductOpenSource,
		IssueDescriptionURL: issue.CreateIssueURL(),
		IssueType:           vulnmap.DependencyVulnerability,
		CodeActions:         issue.AddCodeActions(learnService, ep),
		Ecosystem:           issue.PackageManager,
		CWEs:                issue.Identifiers.CWE,
		CVEs:                issue.Identifiers.CVE,
		AdditionalData:      issue.toAdditionalData(affectedFilePath, scanResult),
	}
}

func (o ossIssue) toAdditionalData(filepath string, scanResult *scanResult) vulnmap.OssIssueData {
	var additionalData vulnmap.OssIssueData
	additionalData.Key = o.Id
	additionalData.Title = o.Title
	additionalData.Name = o.Name
	additionalData.LineNumber = o.LineNumber
	additionalData.Description = o.Description
	additionalData.References = o.toReferences()
	additionalData.Version = o.Version
	additionalData.License = o.License
	additionalData.PackageManager = o.PackageManager
	additionalData.PackageName = o.PackageName
	additionalData.From = o.From
	additionalData.FixedIn = o.FixedIn
	additionalData.UpgradePath = o.UpgradePath
	additionalData.IsUpgradable = o.IsUpgradable
	additionalData.CVSSv3 = o.CVSSv3
	additionalData.CvssScore = o.CvssScore
	additionalData.Exploit = o.Exploit
	additionalData.IsPatchable = o.IsPatchable
	additionalData.ProjectName = scanResult.ProjectName
	additionalData.DisplayTargetFile = scanResult.DisplayTargetFile
	additionalData.Language = o.Language
	additionalData.Details = getDetailsHtml(&o)

	return additionalData
}

func (o ossIssue) toReferences() []vulnmap.Reference {
	var references []vulnmap.Reference
	for _, ref := range o.References {
		references = append(references, ref.toReference())
	}
	return references
}

func (r reference) toReference() vulnmap.Reference {
	url, err := url.Parse(string(r.Url))
	if err != nil {
		log.Err(err).Msg("Unable to parse reference url: " + string(r.Url))
	}
	return vulnmap.Reference{
		Url:   url,
		Title: r.Title,
	}
}

func convertScanResultToIssues(
	res *scanResult,
	path string,
	fileContent []byte,
	ls learn.Service,
	ep error_reporting.ErrorReporter,
	packageIssueCache map[string][]vulnmap.Issue,
) []vulnmap.Issue {
	var issues []vulnmap.Issue

	duplicateCheckMap := map[string]bool{}

	for _, issue := range res.Vulnerabilities {
		packageKey := issue.PackageName + "@" + issue.Version
		duplicateKey := issue.Id + "|" + issue.PackageName
		if duplicateCheckMap[duplicateKey] {
			continue
		}
		issueRange := findRange(issue, path, fileContent)
		vulnmapIssue := toIssue(path, issue, res, issueRange, ls, ep)
		packageIssueCache[packageKey] = append(packageIssueCache[packageKey], vulnmapIssue)
		issues = append(issues, vulnmapIssue)
		duplicateCheckMap[duplicateKey] = true
	}
	return issues
}
