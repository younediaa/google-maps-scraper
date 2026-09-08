# Google Maps Scraper on Replit

## Run

The `Start application` workflow runs the existing Go browser UI on Replit's preview port:

```sh
GOSUMDB=sum.golang.org go run . -web -addr 0.0.0.0:5000
```

Scrape output and job state are stored in the project's default `webdata` directory. No API key is required for the core browser UI. Proxies are optional and can be entered in the web form.