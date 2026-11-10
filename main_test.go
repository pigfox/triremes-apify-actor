package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var ctx = context.Background()

// score is n as the API's optional score.
func score(n int) *int { return &n }

// triremes is a fake API v1: two pages of sites, the second behind a
// cursor, for the key "good"; any other key is refused as the API does.
func triremes(t *testing.T, asked *[]string) *httptest.Server {
	pages := map[string]any{
		"": map[string]any{"sites": []site{
			{ID: "a1", URL: "https://triremes.example/website-gaps/site/a1", Title: "Dentist in Reno, NV, United States", Category: "Dentist",
				City: "Reno", Region: "NV", Country: "US", MeasuredAt: "2026-11-02T09:00:00Z", Price: "$99", SearchEngineScore: score(64),
				FailedChecks: []string{"Meta description"}, AnswerEngineScore: score(40), AnswerEngineFailed: []string{"llms.txt"}, Sold: 2},
			{ID: "a2", Title: "Dentist in Reno, NV, United States", SearchEngineScore: score(12)},
			{ID: "a3", Title: "Dentist in Reno, NV, United States", BeingMeasured: true},
		}, "next_cursor": "c2"},
		"c2": map[string]any{"sites": []site{{ID: "a4", Title: "Dentist in Sparks, NV, United States", SearchEngineScore: score(88), Hot: true}},
			"next_cursor": nil},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked = append(*asked, r.URL.RawQuery)
		if r.Header.Get("User-Agent") != UserAgent || r.URL.Path != "/api/v1/website-gaps" {
			t.Errorf("%s %s", r.Header.Get("User-Agent"), r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			mustWrite(t, w, `{"error":{"code":"unauthorized","message":"No such API key."}}`)
			return
		}
		if err := json.NewEncoder(w).Encode(pages[r.URL.Query().Get("cursor")]); err != nil {
			t.Error(err)
		}
	}))
}

func mustWrite(t *testing.T, w io.Writer, text string) {
	t.Helper()
	if _, err := io.WriteString(w, text); err != nil {
		t.Error(err)
	}
}

// environment is getenv over values.
func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// local makes a local storage folder with input as its INPUT.json.
func local(t *testing.T, input string) string {
	t.Helper()
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, "key_value_stores", "default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "key_value_stores", "default", "INPUT.json"), []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

// TestLocalRun: the input's filters asked of the API, every page followed,
// the minimum score and the limit kept, and each site written as a
// dataset row with its scores, checks, date and page, never which
// business or website.
func TestLocalRun(t *testing.T) {
	var asked []string
	api := triremes(t, &asked)
	defer api.Close()
	folder := local(t, `{"apiKey":"good","category":"Dentist","country":"US","region":"NV","q":"reno","gap":"meta_description","minimumScore":50,"limit":10}`)
	var out, errs bytes.Buffer
	if code := run(ctx, environment(map[string]string{"APIFY_LOCAL_STORAGE_DIR": folder, "TRIREMES_API_URL": api.URL + "/api/v1/"}),
		api.Client(), &out, &errs); code != 0 || out.String() != "triremes-apify: done\n" {
		t.Fatal(code, out.String(), errs.String())
	}
	if len(asked) != 2 || asked[0] != "category=Dentist&country=US&gap=meta_description&limit=50&q=reno&region=NV" ||
		!strings.HasSuffix(asked[1], "&cursor=c2") && !strings.Contains(asked[1], "cursor=c2") {
		t.Fatal(asked)
	}
	rows, err := filepath.Glob(filepath.Join(folder, "datasets", "default", "*.json"))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	text, err := os.ReadFile(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var first Item
	if err := json.Unmarshal(text, &first); err != nil {
		t.Fatal(err)
	}
	if first.ID != "a1" || *first.SEOScore != 64 || *first.AEOScore != 40 || first.SEOFailedChecks[0] != "Meta description" ||
		first.AEOFailedChecks[0] != "llms.txt" || first.SoldTimes != 2 || first.LeadURL != "https://triremes.example/website-gaps/site/a1" ||
		first.MeasuredAt != "2026-11-02T09:00:00Z" || first.Price != "$99" {
		t.Fatalf("%+v", first)
	}
	if strings.Contains(string(text), "Shop") || !strings.Contains(string(text), `"business_and_website": "shown once bought, at lead_url"`) {
		t.Fatal(string(text))
	}
	// example-output.json is this run's dataset, as the README shows it.
	var rowsRun []Item
	for _, row := range rows {
		var item Item
		text, err := os.ReadFile(row)
		if err == nil {
			err = json.Unmarshal(text, &item)
		}
		if err != nil {
			t.Fatal(err)
		}
		rowsRun = append(rowsRun, item)
	}
	var example []Item
	text, err = os.ReadFile("example-output.json")
	if err == nil {
		err = json.Unmarshal(text, &example)
	}
	if err != nil || !reflect.DeepEqual(example, rowsRun) {
		t.Fatalf("example-output.json is not this run's dataset: %v\n%+v", err, rowsRun)
	}
	// The limit stops the walk; no minimum keeps the sites being measured.
	asked = nil
	items, err := list(ctx, api.Client(), api.URL+"/api/v1", Input{APIKey: "good", Limit: 3})
	if err != nil || len(items) != 3 || items[2].ID != "a3" || !items[2].BeingMeasured || len(items[2].SEOFailedChecks) != 0 || len(asked) != 1 {
		t.Fatal(items, err, asked)
	}
}

// TestApifyRun: on Apify the input comes from the run's key-value store and
// the rows go to its dataset, each with the run's token.
func TestApifyRun(t *testing.T) {
	var asked []string
	api := triremes(t, &asked)
	defer api.Close()
	var pushed []Item
	apify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer run-token" {
			t.Error(r.Header)
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v2/key-value-stores/store-1/records/INPUT":
			mustWrite(t, w, `{"apiKey":"good","limit":2}`)
		case "POST /v2/datasets/dataset-1/items":
			if err := json.NewDecoder(r.Body).Decode(&pushed); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Error(r.Method, r.URL.Path)
		}
	}))
	defer apify.Close()
	env := environment(map[string]string{"APIFY_TOKEN": "run-token", "APIFY_DEFAULT_KEY_VALUE_STORE_ID": "store-1",
		"APIFY_DEFAULT_DATASET_ID": "dataset-1", "APIFY_API_PUBLIC_BASE_URL": apify.URL, "TRIREMES_API_URL": api.URL + "/api/v1"})
	var out, errs bytes.Buffer
	if code := run(ctx, env, api.Client(), &out, &errs); code != 0 || len(pushed) != 2 || pushed[1].ID != "a2" {
		t.Fatal(code, pushed, errs.String())
	}
}

// failing is a writer that fails.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("closed") }

// TestFailures: each failure ends the run with its reason, and a reason
// that cannot be written either with 2.
func TestFailures(t *testing.T) {
	var asked []string
	api := triremes(t, &asked)
	defer api.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		mustWrite(t, w, "not json")
	}))
	defer broken.Close()
	notJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mustWrite(t, w, "[") }))
	defer notJSON.Close()
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		mustWrite(t, w, "{")
	}))
	defer short.Close()
	for _, c := range []struct {
		name, input, api, want string
		apify                  map[string]string
	}{
		{"no input", "", api.URL, "input: open", nil},
		{"input not JSON", "{", api.URL, "input: unexpected end of JSON input", nil},
		{"no key", `{"limit":5}`, api.URL, "apiKey: your Triremes API key", nil},
		{"no limit", `{"apiKey":"good"}`, api.URL, "limit: at least 1", nil},
		{"key refused", `{"apiKey":"bad","limit":5}`, api.URL, "triremes: 401 Unauthorized: No such API key.", nil},
		{"API down", `{"apiKey":"good","limit":5}`, broken.URL, "triremes: 502 Bad Gateway", nil},
		{"API not JSON", `{"apiKey":"good","limit":5}`, notJSON.URL, "triremes: unexpected end of JSON input", nil},
		{"API cut short", `{"apiKey":"good","limit":5}`, short.URL, "triremes: unexpected EOF", nil},
		{"API unreachable", `{"apiKey":"good","limit":5}`, "http://127.0.0.1:1", "triremes: Get", nil},
		{"API address bad", `{"apiKey":"good","limit":5}`, "http://bad\x7f", "triremes: parse", nil},
		{"Apify store down", "", api.URL, "input: 502 Bad Gateway", map[string]string{"APIFY_TOKEN": "t",
			"APIFY_DEFAULT_KEY_VALUE_STORE_ID": "s", "APIFY_API_PUBLIC_BASE_URL": broken.URL}},
	} {
		env := map[string]string{"TRIREMES_API_URL": c.api + "/api/v1"}
		if c.input != "" {
			env["APIFY_LOCAL_STORAGE_DIR"] = local(t, c.input)
		} else {
			env["APIFY_LOCAL_STORAGE_DIR"] = t.TempDir()
		}
		for name, value := range c.apify {
			env[name] = value
		}
		var out, errs bytes.Buffer
		if code := run(ctx, environment(env), http.DefaultClient, &out, &errs); code != 1 || !strings.Contains(errs.String(), c.want) {
			t.Errorf("%s: %d %q, want %q", c.name, code, errs.String(), c.want)
		}
	}
	// The dataset cannot be written: a file where its folder goes, then a
	// file where a row goes.
	folder := local(t, `{"apiKey":"good","limit":1}`)
	if err := os.WriteFile(filepath.Join(folder, "datasets"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	env := environment(map[string]string{"APIFY_LOCAL_STORAGE_DIR": folder, "TRIREMES_API_URL": api.URL + "/api/v1"})
	if code := run(ctx, env, api.Client(), &out, &errs); code != 1 || !strings.Contains(errs.String(), "dataset: mkdir") {
		t.Fatal(code, errs.String())
	}
	folder = local(t, `{"apiKey":"good","limit":1}`)
	if err := os.MkdirAll(filepath.Join(folder, "datasets", "default", "000000001.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	env = environment(map[string]string{"APIFY_LOCAL_STORAGE_DIR": folder, "TRIREMES_API_URL": api.URL + "/api/v1"})
	if code := run(ctx, env, api.Client(), &out, &errs); code != 1 || !strings.Contains(errs.String(), "is a directory") {
		t.Fatal(code, errs.String())
	}
	// Done, but stdout closed: 1; and stderr closed too: 2.
	folder = local(t, `{"apiKey":"good","limit":1}`)
	env = environment(map[string]string{"APIFY_LOCAL_STORAGE_DIR": folder, "TRIREMES_API_URL": api.URL + "/api/v1"})
	if code := run(ctx, env, api.Client(), failing{}, &errs); code != 1 {
		t.Fatal(code)
	}
	if code := run(ctx, env, api.Client(), failing{}, failing{}); code != 2 {
		t.Fatal(code)
	}
}

// TestMain: main runs the actor from the process's environment and exits
// with its code: here 1, with no input in the working directory's storage.
func TestMainExits(t *testing.T) {
	t.Setenv("APIFY_LOCAL_STORAGE_DIR", t.TempDir())
	t.Setenv("APIFY_TOKEN", "")
	code := -1
	exit = func(c int) { code = c }
	defer func() { exit = os.Exit }()
	main()
	if code != 1 {
		t.Fatal(code)
	}
	if base("", "x") != "x" || base("y/", "x") != "y" {
		t.Fatal("base")
	}
}
