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

## API

```sh
./bin/nearby serve -addr :8080
curl 'localhost:8080/search?q=ramen&city=Tokyo&limit=5'
curl 'localhost:8080/search?q=ramen&city=Tokyo&format=csv'
```

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

## Proxies

`-proxies http://user:pass@host:port,socks5://host:port` or `-proxies @proxies.txt` (one per line). Each search uses the next one.

## License

MIT
