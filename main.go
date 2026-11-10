// Command apify is the Apify actor "Triremes website-gap leads": it lists
// the business websites on sale on Triremes that match its input, through
// Triremes' API v1 with the user's own API key, and writes each to the
// run's dataset. A listing names the kind of business, where it is, its
// SEO and AEO scores and failed checks, the day it was measured and its
// page on Triremes, never which business or website: that is the lead,
// bought on Triremes (its page, or POST /api/v1/purchases).
//
// It uses the standard library only, so the actor builds from this folder
// alone (Dockerfile). On Apify it reads its input from the run's key-value
// store and pushes to its dataset through Apify's API (APIFY_TOKEN and
// the store's and dataset's ids, which Apify sets); run anywhere else, it
// reads storage/key_value_stores/default/INPUT.json and writes
// storage/datasets/default/, as Apify's CLI lays them out.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// UserAgent names the actor to Triremes, whose radar counts it by name.
const UserAgent = "triremes-apify/1.0"

// The services it calls, unless the environment points elsewhere (tests,
// Apify's own APIFY_API_PUBLIC_BASE_URL).
const (
	triremesAPI = "https://triremes.com/api/v1"
	apifyAPI    = "https://api.apify.com"
)

// pageSize is the most sites API v1 answers a call: its own limit.
const pageSize = 50

// Input is the actor's input (.actor/input_schema.json).
type Input struct {
	APIKey       string `json:"apiKey"`
	Category     string `json:"category"`
	Country      string `json:"country"`
	Region       string `json:"region"`
	City         string `json:"city"`
	Keyword      string `json:"q"`
	Gap          string `json:"gap"`
	MinimumScore int    `json:"minimumScore"`
	Limit        int    `json:"limit"`
}

// site is a site on sale as API v1 answers it: the fields the actor keeps.
type site struct {
	ID                 string   `json:"id"`
	URL                string   `json:"url"`
	Title              string   `json:"title"`
	Category           string   `json:"category"`
	Region             string   `json:"region"`
	City               string   `json:"city"`
	Country            string   `json:"country"`
	MeasuredAt         string   `json:"measured_at"`
	Price              string   `json:"price"`
	SearchEngineScore  *int     `json:"search_engine_score"`
	FailedChecks       []string `json:"failed_checks"`
	AnswerEngineScore  *int     `json:"answer_engine_score"`
	AnswerEngineFailed []string `json:"answer_engine_failed_checks"`
	BeingMeasured      bool     `json:"being_measured"`
	Sold               int      `json:"sold"`
	Hot                bool     `json:"hot"`
}

// Item is one dataset row.
type Item struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Category          string   `json:"category"`
	City              string   `json:"city"`
	Region            string   `json:"region"`
	Country           string   `json:"country"`
	SEOScore          *int     `json:"seo_score"`
	SEOFailedChecks   []string `json:"seo_failed_checks"`
	AEOScore          *int     `json:"aeo_score"`
	AEOFailedChecks   []string `json:"aeo_failed_checks"`
	BeingMeasured     bool     `json:"being_measured"`
	MeasuredAt        string   `json:"measured_at"`
	Price             string   `json:"price"`
	SoldTimes         int      `json:"sold_times"`
	Hot               bool     `json:"hot"`
	LeadURL           string   `json:"lead_url"`
	BusinessAndSiteAt string   `json:"business_and_website"`
}

// exit is os.Exit; tests replace it.
var exit = os.Exit

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Getenv, http.DefaultClient, os.Stdout, os.Stderr)
	stop()
	exit(code)
}

// run reads the input, lists the sites and writes them: 0 when done, 1
// with the reason on stderr when not, 2 when stderr could not be written
// either.
func run(ctx context.Context, getenv func(string) string, client *http.Client, stdout, stderr io.Writer) int {
	err := write(ctx, getenv, client)
	if err == nil {
		_, err = fmt.Fprintln(stdout, "triremes-apify: done")
	}
	if err == nil {
		return 0
	}
	if _, failed := fmt.Fprintln(stderr, "triremes-apify:", err); failed != nil {
		return 2
	}
	return 1
}

// write reads the input, lists the sites and writes them to the dataset.
func write(ctx context.Context, getenv func(string) string, client *http.Client) error {
	store := storage{getenv: getenv, client: client}
	in, err := store.input(ctx)
	if err != nil {
		return fmt.Errorf("input: %w", err)
	}
	items, err := list(ctx, client, base(getenv("TRIREMES_API_URL"), triremesAPI), in)
	if err != nil {
		return err
	}
	if err := store.push(ctx, items); err != nil {
		return fmt.Errorf("dataset: %w", err)
	}
	return nil
}

// base is value without a trailing slash, else fallback.
func base(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return strings.TrimRight(value, "/")
}

// list asks API v1 for the sites in, a page at a time, until in.Limit are
// kept or none are left; a site below in.MinimumScore, or without an SEO
// score while one is asked for, is left out.
func list(ctx context.Context, client *http.Client, api string, in Input) ([]Item, error) {
	if strings.TrimSpace(in.APIKey) == "" {
		return nil, errors.New("apiKey: your Triremes API key (triremes.com/account/api) is required")
	}
	if in.Limit < 1 {
		return nil, errors.New("limit: at least 1")
	}
	query := url.Values{"limit": {strconv.Itoa(pageSize)}}
	for name, value := range map[string]string{"category": in.Category, "country": in.Country, "region": in.Region,
		"city": in.City, "q": in.Keyword, "gap": in.Gap} {
		if value != "" {
			query.Set(name, value)
		}
	}
	items := []Item{}
	for {
		var page struct {
			Sites      []site  `json:"sites"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := call(ctx, client, http.MethodGet, api+"/website-gaps?"+query.Encode(), "Bearer "+in.APIKey, nil, &page); err != nil {
			return nil, fmt.Errorf("triremes: %w", err)
		}
		for _, s := range page.Sites {
			if in.MinimumScore > 0 && (s.SearchEngineScore == nil || *s.SearchEngineScore < in.MinimumScore) {
				continue
			}
			items = append(items, itemOf(s))
			if len(items) == in.Limit {
				return items, nil
			}
		}
		if page.NextCursor == nil {
			return items, nil
		}
		query.Set("cursor", *page.NextCursor)
	}
}

// itemOf is s as a dataset row.
func itemOf(s site) Item {
	return Item{ID: s.ID, Title: s.Title, Category: s.Category, City: s.City, Region: s.Region, Country: s.Country,
		SEOScore: s.SearchEngineScore, SEOFailedChecks: nonNil(s.FailedChecks), AEOScore: s.AnswerEngineScore,
		AEOFailedChecks: nonNil(s.AnswerEngineFailed), BeingMeasured: s.BeingMeasured, MeasuredAt: s.MeasuredAt, Price: s.Price,
		SoldTimes: s.Sold, Hot: s.Hot, LeadURL: s.URL, BusinessAndSiteAt: "shown once bought, at lead_url"}
}

// nonNil is list, or an empty one for none.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// call sends one request and decodes a 2xx answer's JSON into out (when
// set); any other answer is an error with its status and message.
func call(ctx context.Context, client *http.Client, method, address, authorization string, body []byte, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", UserAgent)
	request.Header.Set("Accept", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	text, err := io.ReadAll(response.Body)
	err = errors.Join(err, response.Body.Close())
	if err != nil {
		return err
	}
	if response.StatusCode/100 != 2 {
		var refused struct {
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal(text, &refused) == nil && refused.Error.Message != "" {
			return fmt.Errorf("%s: %s", response.Status, refused.Error.Message)
		}
		return errors.New(response.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(text, out)
}

// storage is the run's input and dataset: Apify's, or the local folders.
type storage struct {
	getenv func(string) string
	client *http.Client
}

// onApify reports whether the run is Apify's.
func (s storage) onApify() bool {
	return s.getenv("APIFY_TOKEN") != "" && s.getenv("APIFY_DEFAULT_KEY_VALUE_STORE_ID") != ""
}

// local is a path under the local storage folder.
func (s storage) local(parts ...string) string {
	return filepath.Join(append([]string{base(s.getenv("APIFY_LOCAL_STORAGE_DIR"), "storage")}, parts...)...)
}

// input reads the run's input.
func (s storage) input(ctx context.Context) (Input, error) {
	key := base(s.getenv("APIFY_INPUT_KEY"), "INPUT")
	var in Input
	if s.onApify() {
		address := base(s.getenv("APIFY_API_PUBLIC_BASE_URL"), apifyAPI) + "/v2/key-value-stores/" +
			url.PathEscape(s.getenv("APIFY_DEFAULT_KEY_VALUE_STORE_ID")) + "/records/" + url.PathEscape(key)
		return in, call(ctx, s.client, http.MethodGet, address, "Bearer "+s.getenv("APIFY_TOKEN"), nil, &in)
	}
	text, err := os.ReadFile(s.local("key_value_stores", "default", key+".json"))
	if err != nil {
		return in, err
	}
	return in, json.Unmarshal(text, &in)
}

// push writes items to the run's dataset.
func (s storage) push(ctx context.Context, items []Item) error {
	if s.onApify() {
		body, err := json.Marshal(items)
		if err == nil {
			address := base(s.getenv("APIFY_API_PUBLIC_BASE_URL"), apifyAPI) + "/v2/datasets/" +
				url.PathEscape(s.getenv("APIFY_DEFAULT_DATASET_ID")) + "/items"
			err = call(ctx, s.client, http.MethodPost, address, "Bearer "+s.getenv("APIFY_TOKEN"), body, nil)
		}
		return err
	}
	folder := s.local("datasets", "default")
	err := os.MkdirAll(folder, 0o750)
	for i := 0; err == nil && i < len(items); i++ {
		var text []byte
		if text, err = json.MarshalIndent(items[i], "", "  "); err == nil {
			err = os.WriteFile(filepath.Join(folder, fmt.Sprintf("%09d.json", i+1)), append(text, '\n'), 0o600)
		}
	}
	return err
}
