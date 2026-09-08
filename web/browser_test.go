//nolint:testpackage,wsl // browser tests use production handlers and intentionally linear UI steps
package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

const browserJobID = "33333333-3333-3333-3333-333333333333"

var (
	installBrowserDriverOnce sync.Once
	installBrowserDriverErr  error
)

type browserFixture struct {
	mu         sync.Mutex
	jobsCalls  int
	submission url.Values
}

func newBrowserFixture(t *testing.T) (*httptest.Server, *browserFixture) {
	t.Helper()

	dir := t.TempDir()
	csv := "title,review_rating,review_count,phone,website,address,category,latitude,longitude\n" +
		"Low Rated,3.2,250,111,https://low.example,First Street,Cafe,1.5,2.5\n" +
		"Top Rated,4.9,12,222,https://top.example,Second Street,Bakery,3.5,4.5\n"
	if err := os.WriteFile(filepath.Join(dir, browserJobID+".csv"), []byte(csv), 0o600); err != nil {
		t.Fatalf("write browser fixture CSV: %v", err)
	}

	app := newTestServer(t, dir)
	fixture := &browserFixture{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			fixture.mu.Lock()
			fixture.jobsCalls++
			call := fixture.jobsCalls
			fixture.mu.Unlock()
			status := "pending"
			actions := `<button class="delete-button">Delete</button>`
			if call > 1 {
				status = "ok"
				actions = fmt.Sprintf(`<button type="button" class="button view-button" onclick="window.currentJobId='%s'" hx-get="/view?id=%s" hx-target="#map-modal-container" hx-swap="innerHTML">View</button>`, browserJobID, browserJobID)
			}
			fmt.Fprintf(w, `<tr data-job-id="%s" data-status="%s"><td>%s</td><td>Browser job</td><td>today</td><td><span class="status-indicator status-%s">%s</span></td><td>%s</td></tr>`,
				browserJobID, status, browserJobID, status, status, actions)
		case "/scrape":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fixture.mu.Lock()
			fixture.submission = r.Form
			fixture.mu.Unlock()
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<tr data-job-id="submitted" data-status="pending"><td>submitted</td><td>UI request</td><td>today</td><td>pending</td><td></td></tr>`)
		default:
			app.srv.Handler.ServeHTTP(w, r)
		}
	})

	return httptest.NewServer(handler), fixture
}

func launchBrowser(t *testing.T) (browser playwright.Browser, cleanup func()) {
	t.Helper()

	executable := os.Getenv("CHROMIUM_PATH")
	if executable == "" {
		replitChromium := "/repl/tools/bin/chromium"
		if _, statErr := os.Stat(replitChromium); statErr == nil {
			executable = replitChromium
		}
	}

	pw, err := playwright.Run()
	if err != nil {
		installBrowserDriverOnce.Do(func() {
			options := &playwright.RunOptions{SkipInstallBrowsers: executable != ""}
			if executable == "" {
				options.Browsers = []string{"chromium"}
			}
			installBrowserDriverErr = playwright.Install(options)
		})
		if installBrowserDriverErr != nil {
			t.Fatalf("install Playwright driver: %v", installBrowserDriverErr)
		}
		pw, err = playwright.Run()
		if err != nil {
			t.Fatalf("start Playwright: %v", err)
		}
	}
	launchOptions := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)}
	if executable != "" {
		launchOptions.ExecutablePath = playwright.String(executable)
	}
	browser, err = pw.Chromium.Launch(launchOptions)
	if err != nil {
		_ = pw.Stop()
		t.Fatalf("launch Chromium: %v", err)
	}
	return browser, func() {
		_ = browser.Close()
		_ = pw.Stop()
	}
}

func waitFor(t *testing.T, page playwright.Page, expression string) {
	t.Helper()
	if _, err := page.WaitForFunction(expression, nil, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("wait for %q: %v", expression, err)
	}
}

func TestBrowserFormSubmissionPreservesRequestFields(t *testing.T) {
	server, fixture := newBrowserFixture(t)
	defer server.Close()
	browser, closeBrowser := launchBrowser(t)
	defer closeBrowser()

	page, err := browser.NewPage(playwright.BrowserNewPageOptions{
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	})
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	if _, err = page.Goto(server.URL); err != nil {
		t.Fatalf("open app: %v", err)
	}
	waitFor(t, page, `document.querySelector('#job-table tbody tr') !== null`)
	for _, summary := range []string{"Location Settings", "Advanced Options", "Proxies"} {
		if err := page.GetByText(summary, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).Click(); err != nil {
			t.Fatalf("open %s: %v", summary, err)
		}
	}

	values := map[string]string{
		"#name": "UI request", "#keywords": "coffee\nbakery", "#maxtime": "12m",
		"#zoom": "14", "#latitude": "-36.8485", "#longitude": "174.7633",
		"#radius": "4321", "#proxies": "http://proxy.example:8080",
	}
	for selector, value := range values {
		if err := page.Locator(selector).Fill(value); err != nil {
			t.Fatalf("fill %s: %v", selector, err)
		}
	}
	for selector, value := range map[string]string{"#lang": "fr", "#depth": "5"} {
		if _, err := page.Locator(selector).SelectOption(playwright.SelectOptionValues{Values: &[]string{value}}); err != nil {
			t.Fatalf("select %s: %v", selector, err)
		}
	}
	for _, selector := range []string{"#fastmode", "#email"} {
		if err := page.Locator(selector).Check(); err != nil {
			t.Fatalf("check %s: %v", selector, err)
		}
	}
	if err := page.Locator(".submit-button").Click(); err != nil {
		t.Fatalf("submit form: %v", err)
	}
	waitFor(t, page, `document.querySelector('[data-job-id="submitted"]') !== null`)

	fixture.mu.Lock()
	got := fixture.submission
	fixture.mu.Unlock()
	want := url.Values{
		"name": {"UI request"}, "keywords": {"coffee\nbakery"}, "lang": {"fr"}, "depth": {"5"},
		"maxtime": {"12m"}, "zoom": {"14"}, "latitude": {"-36.8485"}, "longitude": {"174.7633"},
		"radius": {"4321"}, "proxies": {"http://proxy.example:8080"}, "fastmode": {"on"}, "email": {"on"},
	}
	for field, expected := range want {
		if actual := got.Get(field); actual != expected[0] {
			t.Errorf("field %s = %q, want %q", field, actual, expected[0])
		}
	}
}

func TestBrowserPollingResultsSortingAndResponsiveLayouts(t *testing.T) {
	for _, viewport := range []struct {
		name          string
		width, height int
		mobile        bool
	}{
		{name: "desktop", width: 1280, height: 900},
		{name: "mobile", width: 390, height: 844, mobile: true},
	} {
		t.Run(viewport.name, func(t *testing.T) {
			server, _ := newBrowserFixture(t)
			defer server.Close()
			browser, closeBrowser := launchBrowser(t)
			defer closeBrowser()
			page, err := browser.NewPage(playwright.BrowserNewPageOptions{
				Viewport: &playwright.Size{Width: viewport.width, Height: viewport.height},
			})
			if err != nil {
				t.Fatalf("new page: %v", err)
			}
			if _, err = page.Goto(server.URL); err != nil {
				t.Fatalf("open app: %v", err)
			}
			waitFor(t, page, `document.querySelector('[data-status="pending"]') !== null`)

			columns, err := page.Locator(".primary-fields").Evaluate(`element => getComputedStyle(element).gridTemplateColumns`, nil)
			if err != nil {
				t.Fatalf("read responsive form layout: %v", err)
			}
			isSingleColumn := !strings.Contains(fmt.Sprint(columns), " ")
			if isSingleColumn != viewport.mobile {
				t.Errorf("single-column layout = %v, want %v (columns %q)", isSingleColumn, viewport.mobile, columns)
			}

			if _, err := page.Evaluate(`htmx.ajax('GET', '/jobs', {target:'#job-table tbody', swap:'innerHTML'})`); err != nil {
				t.Fatalf("trigger job poll: %v", err)
			}
			waitFor(t, page, `document.querySelector('[data-status="ok"]') !== null`)
			waitFor(t, page, `document.getElementById('success-toast').classList.contains('show')`)

			if err := page.Locator(".view-button").Click(); err != nil {
				t.Fatalf("open results: %v", err)
			}
			waitFor(t, page, `document.querySelectorAll('#results-table tbody tr').length === 2`)
			href, err := page.Locator("#results-export").GetAttribute("href")
			if err != nil || href != "/download?id="+browserJobID {
				t.Fatalf("export href = %q, err %v", href, err)
			}
			count, err := page.Locator("#results-count").TextContent()
			if err != nil || count != "2 businesses found" {
				t.Fatalf("results count = %q, err %v", count, err)
			}

			if err := page.Locator(`[data-sort="review_rating"]`).Click(); err != nil {
				t.Fatalf("sort ratings: %v", err)
			}
			first, _ := page.Locator("#results-table tbody tr").First().Locator("td").First().TextContent()
			if first != "Top Rated" {
				t.Errorf("rating descending first row = %q, want Top Rated", first)
			}
			if err := page.Locator(`[data-sort="review_count"]`).Click(); err != nil {
				t.Fatalf("sort reviews: %v", err)
			}
			first, _ = page.Locator("#results-table tbody tr").First().Locator("td").First().TextContent()
			if first != "Low Rated" {
				t.Errorf("review descending first row = %q, want Low Rated", first)
			}

			direction, err := page.Locator(".results-toolbar").Evaluate(`element => getComputedStyle(element).flexDirection`, nil)
			if err != nil {
				t.Fatalf("read modal layout: %v", err)
			}
			wantDirection := "row"
			if viewport.mobile {
				wantDirection = "column"
			}
			if direction != wantDirection {
				t.Errorf("results toolbar direction = %q, want %q", direction, wantDirection)
			}
		})
	}
}
