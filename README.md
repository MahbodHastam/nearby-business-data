# nearby-business-data

Scrapes Google Maps for businesses in a city (e.g. "coffee in Berlin") and gives you name, category, rating, reviews, address, phone, website, opening hours and coordinates for each one.

Use it as a CLI that writes JSON/CSV, or run it as an HTTP API.

Needs Go 1.26+ and Chrome installed.

## CLI

```sh
make build
./bin/nearby scrape -q coffee -city Berlin -o coffee.json
./bin/nearby scrape -q dentist -city Toronto -limit 50 -o dentists.csv
```

Flags: `-limit` (default 20, 0 for all), `-o`, `-format json|csv`, `-concurrency` (default 4), `-proxies`, `-timeout`, `-headful`.

### Search around a point

```sh
./bin/nearby scrape -q coffee -near 52.5219,13.4132 -radius 1500 -sort distance -limit 10
```

`-near lat,lng` replaces `-city`. Each result gets `distance_m`. `-radius` (meters) sizes the map view and drops places farther away.

### Filters and sorting

| Flag | Meaning |
|---|---|
| `-min-rating 4.5` | rating at least this |
| `-min-reviews 100` | at least this many reviews |
| `-category cafe` | category contains this text, case-insensitive |
| `-open-now` | open when the page was read |
| `-sort rating\|reviews\|distance\|name` | rating and reviews high to low, distance near to far (needs `-near`) |

Filters and sorting pick from every place Maps lists for the search (about 100-120), then `-limit` applies. That scrapes more pages than a plain search, so it takes longer.

## API

```sh
./bin/nearby serve -addr :8080
curl 'localhost:8080/search?q=ramen&city=Tokyo&limit=5'
curl 'localhost:8080/search?q=ramen&city=Tokyo&format=csv'
curl 'localhost:8080/search?q=cafe&lat=52.5219&lng=13.4132&radius=1500&min_rating=4.5&sort=distance'
```

Query parameters:

| Parameter | Meaning |
|---|---|
| `q` | what to search for (required) |
| `city` or `lat` + `lng` | where (one of them is required) |
| `radius` | meters, 1-50000; needs `lat`/`lng`; drops farther places |
| `limit` | 1-100, default 20 |
| `min_rating`, `min_reviews`, `category`, `open_now` | filters, as in the CLI |
| `sort` | `rating`, `reviews`, `distance` or `name` |
| `format` | `json` (default) or `csv` |

`open_now` comes from the place page when it was scraped, so a cached result can be up to `-cache-ttl` old.

```json
{
  "query": "ramen",
  "city": "Tokyo",
  "count": 5,
  "results": [
    {
      "name": "Gyumon Halal Wagyu Ramen Shibuya",
      "category": "Halal restaurant",
      "rating": 4.9,
      "reviews": 30249,
      "address": "Japan, 〒150-0002 Tokyo, Shibuya, 2 Chome−19−17 第106ビル 地下1階",
      "phone": "+818047400868",
      "website": "https://gyumon-group.com/shop/ramen-shibuya/",
      "hours": ["Monday: 12–11:30 pm", "..."],
      "lat": 35.6597321,
      "lng": 139.704161,
      "place_id": "ChIJ_-R_mUGLGGARDfQ8ATZZ4w0",
      "url": "https://www.google.com/maps/place/Gyumon+Halal+Wagyu+Ramen+Shibuya/data=..."
    }
  ]
}
```

### Caching

`serve` keeps results for `-cache-ttl` (default 10m, 0 turns it off) and holds up to `-cache-size` searches (default 256). The key is the query, city or point, radius and the number of places scraped. Filters and sorting run on the cached results, so requests that differ only in those share one scrape. Identical searches that arrive while one is running wait for it instead of starting another Chrome. Cache hits don't use a `-max-jobs` slot.

The `X-Cache` response header says `hit`, `miss` or `coalesced`. Partial results (a search that timed out) are not cached.

### Metrics

`GET /metrics` returns Prometheus text format:

| Metric | Type |
|---|---|
| `nearby_http_requests_total{route,code}`, `nearby_http_request_duration_seconds{route}` | counter, histogram |
| `nearby_searches_total{result=ok\|partial\|error}`, `nearby_search_duration_seconds` | counter, histogram |
| `nearby_chrome_start_duration_seconds` | histogram |
| `nearby_collect_links_duration_seconds`, `nearby_scrape_places_duration_seconds` | histogram |
| `nearby_places_found_total`, `nearby_places_scraped_total`, `nearby_places_failed_total` | counter |
| `nearby_cache_requests_total{status=hit\|miss\|coalesced}` | counter |
| `nearby_searches_in_flight`, `nearby_busy_rejections_total` | gauge, counter |

`/metrics` has no authentication; don't expose the server to the internet without a proxy in front.

## Proxies

`-proxies http://user:pass@host:port,socks5://host:port` or `-proxies @proxies.txt` (one per line). Each search uses the next one.

## License

MIT
