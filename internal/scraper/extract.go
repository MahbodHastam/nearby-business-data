package scraper

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

// Everything that depends on Google Maps markup lives here.

const stateJS = `(() => {
	if (location.hostname.startsWith('consent.')) return 'consent';
	if (document.querySelector('div[role="feed"]')) return 'feed';
	if (location.pathname.startsWith('/maps/place/') && document.querySelector('h1')) return 'place';
	return '';
})()`

// EU visitors get a cookie consent page first.
const consentJS = `(() => {
	const b = [...document.querySelectorAll('form button')].find(b => /accept all/i.test(b.textContent));
	if (b) b.click();
	return !!b;
})()`

const scrollJS = `(() => {
	const feed = document.querySelector('div[role="feed"]');
	if (!feed) return {count: 0, end: true};
	feed.scrollTop = feed.scrollHeight;
	const links = [...feed.querySelectorAll('a[href*="/maps/place/"]')].map(a => a.href.split('?')[0]);
	return {
		count: new Set(links).size,
		end: feed.innerText.includes("You've reached the end of the list"),
	};
})()`

const linksJS = `[...document.querySelectorAll('div[role="feed"] a[href*="/maps/place/"]')].map(a => a.href)`

const placeJS = `(async () => {
	const q = s => document.querySelector(s);
	const label = s => q(s)?.getAttribute('aria-label') ?? '';
	const labels = s => [...document.querySelectorAll(s)].map(e => e.getAttribute('aria-label') ?? '');
	const weekly = () => {
		for (const t of document.querySelectorAll('table')) {
			const rows = [...t.rows]
				.map(r => [...r.cells].map(c => c.innerText.replace(/\s+/g, ' ').trim()))
				.filter(c => c.length >= 2 && /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)/.test(c[0]))
				.map(c => c[0] + ': ' + c[1]);
			if (rows.length) return rows;
		}
		return [];
	};
	const place = {
		name: q('h1')?.textContent ?? '',
		category: q('button[jsaction*="category"]')?.textContent ?? '',
		rating: labels('[role="img"][aria-label]').find(l => /^[\d.,]+ stars?\s*$/.test(l)) ?? '',
		reviews: labels('[aria-label]').find(l => /^[\d.,]+ reviews?$/.test(l.trim())) ?? '',
		address: label('button[data-item-id="address"]'),
		phone: q('button[data-item-id^="phone:tel:"]')?.getAttribute('data-item-id') ?? '',
		website: q('a[data-item-id="authority"]')?.href ?? '',
		hours: weekly(),
		// e.g. "Open · Closes 12 am · See more hours". Some pages have no
		// data-item-id="oh" button; the same text then sits next to the
		// "Show open hours for the week" icon.
		status: label('[data-item-id="oh"]') ||
			(q('[aria-label*="open hours for the week"]')?.parentElement?.innerText ?? '').trim(),
	};
	// Some places only show a summary until "See more hours" is clicked,
	// and that button can show up a bit after the title.
	for (let i = 0; i < 10 && !place.hours.length; i++) {
		q('[aria-label*="See more hours"]')?.click();
		await new Promise(r => setTimeout(r, 200));
		place.hours = weekly();
	}
	return place;
})()`

type scrollState struct {
	Count int  `json:"count"`
	End   bool `json:"end"`
}

type rawPlace struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Rating   string   `json:"rating"`
	Reviews  string   `json:"reviews"`
	Address  string   `json:"address"`
	Phone    string   `json:"phone"`
	Website  string   `json:"website"`
	Hours    []string `json:"hours"`
	Status   string   `json:"status"`
}

var (
	coordsRe  = regexp.MustCompile(`!3d(-?\d+(?:\.\d+)?)!4d(-?\d+(?:\.\d+)?)`)
	placeIDRe = regexp.MustCompile(`!19s([^!?&/]+)`)
	ratingRe  = regexp.MustCompile(`^\d+(?:[.,]\d+)?`)
)

// Coordinates and place ID are read from the link we got from the results
// list; the place page drops the place ID from its URL.
func toBusiness(r rawPlace, placeURL string) business.Business {
	b := business.Business{
		Name:     strings.TrimSpace(r.Name),
		Category: strings.TrimSpace(r.Category),
		Rating:   parseRating(r.Rating),
		Reviews:  parseCount(r.Reviews),
		Address:  strings.TrimSpace(strings.TrimPrefix(r.Address, "Address:")),
		Phone:    strings.TrimPrefix(r.Phone, "phone:tel:"),
		Website:  r.Website,
		Hours:    r.Hours,
		OpenNow:  parseOpenNow(r.Status),
		PlaceID:  parsePlaceID(placeURL),
		URL:      placeURL,
	}
	b.Lat, b.Lng = parseCoords(placeURL)
	return b
}

func parseRating(s string) float64 {
	m := ratingRe.FindString(strings.TrimSpace(s))
	f, _ := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
	return f
}

func parseCount(s string) int {
	n, _ := strconv.Atoi(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s))
	return n
}

func parseCoords(u string) (lat, lng float64) {
	m := coordsRe.FindStringSubmatch(u)
	if m == nil {
		return 0, 0
	}
	lat, _ = strconv.ParseFloat(m[1], 64)
	lng, _ = strconv.ParseFloat(m[2], 64)
	return lat, lng
}

func parsePlaceID(u string) string {
	if m := placeIDRe.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}

// parseOpenNow reads the status line Maps shows above the hours. It returns
// nil when there is none or it isn't recognized.
func parseOpenNow(status string) *bool {
	s := strings.ToLower(strings.TrimSpace(status))
	var open bool
	switch {
	// "opens" must be checked before "open".
	case strings.HasPrefix(s, "closed"), strings.HasPrefix(s, "opens"), strings.HasPrefix(s, "reopens"),
		strings.HasPrefix(s, "temporarily closed"), strings.HasPrefix(s, "permanently closed"):
		open = false
	case strings.HasPrefix(s, "open"), strings.HasPrefix(s, "closes"), strings.HasPrefix(s, "closing soon"):
		open = true
	default:
		return nil
	}
	return &open
}
