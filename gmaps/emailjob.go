package gmaps

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"github.com/mcnijman/go-emailaddress"

	"github.com/gosom/google-maps-scraper/exiter"
)

var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

type EmailExtractJobOptions func(*EmailExtractJob)

type EmailExtractJob struct {
	scrapemate.Job

	Entry                   *Entry
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool
	IsContactPage           bool
	UsageInResults          bool
}

func NewEmailJob(parentID string, entry *Entry, opts ...EmailExtractJobOptions) *EmailExtractJob {
	const (
		defaultPrio       = scrapemate.PriorityHigh
		defaultMaxRetries = 0
	)

	job := EmailExtractJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     "GET",
			URL:        normalizeGoogleURL(entry.WebSite),
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
	}

	job.Entry = entry
	job.UsageInResults = true

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

func WithEmailJobExitMonitor(exitMonitor exiter.Exiter) EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.ExitMonitor = exitMonitor
	}
}

func WithEmailJobWriterManagedCompletion() EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.WriterManagedCompletion = true
	}
}

func (j *EmailExtractJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	completesEntry := true
	defer func() {
		if completesEntry && j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
	}()

	log := scrapemate.GetLoggerFromContext(ctx)

	log.Info("Processing email job", "url", j.URL)

	var emails []string
	if resp.Error == nil {
		if doc, ok := resp.Document.(*goquery.Document); ok {
			emails = append(emails, docEmailExtractor(doc)...)
		}
		emails = append(emails, regexEmailExtractor(resp.Body)...)
	}

	j.Entry.Emails = mergeEmails(j.Entry.Emails, emails)

	if !j.IsContactPage {
		contactURL, ok := contactPageURL(j.URL)
		if ok {
			contactJob := NewEmailContactJob(j.ID, contactURL, j.Entry, j.ExitMonitor, j.WriterManagedCompletion)
			j.UsageInResults = false
			completesEntry = false

			return nil, []scrapemate.IJob{contactJob}, nil
		}
	}

	return j.Entry, nil, nil
}

func (j *EmailExtractJob) ProcessOnFetchError() bool {
	return true
}

func (j *EmailExtractJob) UseInResults() bool {
	return j.UsageInResults
}

func docEmailExtractor(doc *goquery.Document) []string {
	seen := map[string]bool{}

	var emails []string

	doc.Find("a[href^='mailto:']").Each(func(_ int, s *goquery.Selection) {
		mailto, exists := s.Attr("href")
		if exists {
			value := strings.TrimPrefix(mailto, "mailto:")
			if email, err := getValidEmail(value); err == nil {
				if !seen[email] {
					emails = append(emails, email)
					seen[email] = true
				}
			}
		}
	})

	return emails
}

func regexEmailExtractor(body []byte) []string {
	seen := map[string]bool{}

	var emails []string

	addresses := emailPattern.FindAll(body, -1)
	for i := range addresses {
		email := string(addresses[i])
		if !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}

	return emails
}

func NewEmailContactJob(
	parentID, contactURL string,
	entry *Entry,
	exitMonitor exiter.Exiter,
	writerManagedCompletion bool,
) *EmailExtractJob {
	job := NewEmailJob(parentID, entry)
	job.URL = contactURL
	job.IsContactPage = true
	job.ExitMonitor = exitMonitor
	job.WriterManagedCompletion = writerManagedCompletion

	return job
}

func contactPageURL(website string) (string, bool) {
	parsed, err := url.Parse(normalizeGoogleURL(website))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}

	parsed.Path = "/contact"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), true
}

func mergeEmails(groups ...[]string) []string {
	seen := make(map[string]bool)
	emails := make([]string, 0)

	for _, group := range groups {
		for _, email := range group {
			normalized := strings.ToLower(strings.TrimSpace(email))
			if normalized == "" || seen[normalized] {
				continue
			}

			seen[normalized] = true
			emails = append(emails, email)
		}
	}

	return emails
}

func getValidEmail(s string) (string, error) {
	email, err := emailaddress.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}

	return email.String(), nil
}

// normalizeGoogleURL extracts the actual target URL from Google redirect URLs.
// Google Maps sometimes returns URLs like "/url?q=http://example.com/&opi=..."
// for external website links.
func normalizeGoogleURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	if strings.HasPrefix(rawURL, "/url?q=") {
		fullURL := "https://www.google.com" + rawURL

		parsed, err := url.Parse(fullURL)
		if err != nil {
			return rawURL
		}

		if target := parsed.Query().Get("q"); target != "" {
			return target
		}
	}

	if strings.HasPrefix(rawURL, "/") {
		return "https://www.google.com" + rawURL
	}

	return rawURL
}
