package gmaps

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
	"github.com/stretchr/testify/require"
)

func TestEmailExtractJobCollectsHomepageAndContactEmails(t *testing.T) {
	t.Parallel()

	homepage := `<a href="mailto:hello@example.com">Email us</a>
		<p>Sales: sales@example.com</p>`
	homepageDoc, err := goquery.NewDocumentFromReader(strings.NewReader(homepage))
	require.NoError(t, err)

	entry := &Entry{WebSite: "https://example.com/about"}
	job := NewEmailJob("parent", entry)
	homepageResponse := &scrapemate.Response{
		Body:     []byte(homepage),
		Document: homepageDoc,
	}

	result, next, err := job.Process(context.Background(), homepageResponse)

	require.NoError(t, err)
	require.Nil(t, result)
	require.Len(t, next, 1)

	contactJob, ok := next[0].(*EmailExtractJob)
	require.True(t, ok)
	require.True(t, contactJob.IsContactPage)
	require.Equal(t, "https://example.com/contact", contactJob.URL)

	contact := `<p>Contact sales@example.com or help@example.com</p>`
	contactDoc, err := goquery.NewDocumentFromReader(strings.NewReader(contact))
	require.NoError(t, err)

	result, next, err = contactJob.Process(context.Background(), &scrapemate.Response{
		Body:     []byte(contact),
		Document: contactDoc,
	})

	require.NoError(t, err)
	require.Empty(t, next)
	require.Same(t, entry, result)
	require.ElementsMatch(t, []string{
		"hello@example.com",
		"sales@example.com",
		"help@example.com",
	}, entry.Emails)
}

func TestContactPageURLUsesWebsiteOrigin(t *testing.T) {
	t.Parallel()

	got, ok := contactPageURL("https://example.com/some/page?campaign=maps")

	require.True(t, ok)
	require.Equal(t, "https://example.com/contact", got)
}

func TestRegexEmailExtractorUsesRequestedPattern(t *testing.T) {
	t.Parallel()

	got := regexEmailExtractor([]byte("hello first.last+maps@example.co.nz and support@example.com"))

	require.ElementsMatch(t, []string{"first.last+maps@example.co.nz", "support@example.com"}, got)
}
