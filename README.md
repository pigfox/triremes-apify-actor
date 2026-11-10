# Triremes website-gap leads (Apify actor)

Find businesses whose websites fail SEO and AEO checks, by business type
and place, and pull them into your Apify dataset: a ready list of
prospects for an SEO, AEO or web design agency.

Each row is one business website on sale on [Triremes](https://triremes.com):

| Field | What it is |
|---|---|
| `title` | the kind of business and where, for example "Dentist in Reno, NV, United States" |
| `category`, `city`, `region`, `country` | the same, field by field |
| `seo_score` | the SEO score, in % |
| `seo_failed_checks` | the SEO checks its homepage fails, by name |
| `aeo_score` | the AEO score, in %, where Triremes shows one |
| `aeo_failed_checks` | the AEO checks it fails, by name |
| `being_measured` | true while a score is still being measured |
| `measured_at` | when it was measured |
| `price` | what the lead costs on Triremes |
| `sold_times` | how many times the lead has been sold |
| `hot` | a hot lead: its owner confirmed interest |
| `lead_url` | the lead's page on Triremes |

**SEO score:** the share of Triremes' checks of what search engines can
read on this homepage that it fails, weighted by how serious each check
is. 0% means no gaps; higher is worse.

**AEO score (sometimes called GEO):** the share of Triremes' checks of
whether AI answer engines can reach, read and trust this site that it
fails, weighted by how serious each check is. 0% means no gaps; higher is
worse.

A row never names the business or its website: that is the lead. Buy it
at `lead_url` (or through the API's `POST /purchases`) and Triremes gives
you the business, its website and its contact page.

## What you need

A Triremes API key, from [triremes.com/account/api](https://triremes.com/account/api).
API access is a Triremes subscription; [triremes.com/api](https://triremes.com/api)
has the price and the terms. The key is your own: the actor sends it only
to triremes.com, and the input field is secret, so it is never shown in
the run's log or input.

## Input

| Field | |
|---|---|
| Triremes API key | required |
| Business type | for example Dentist or Plumber; empty for every type |
| Country | a two-letter code, for example US; empty for every country |
| State or region | for example NV |
| City | |
| Keyword | words found in the business type, city, region, country or the name of a gap |
| Fails this check | only websites failing one SEO check, for example "No meta description" |
| Minimum SEO score (%) | only websites at or above it; 0 keeps every website |
| Most websites | the run stops at this many |

Example:

```json
{
  "apiKey": "your key",
  "category": "Dentist",
  "country": "US",
  "region": "NV",
  "minimumScore": 50,
  "limit": 100
}
```

## Example run

[`example-output.json`](example-output.json) is the dataset of a run on
the actor's sample data (its tests check that it stays exactly that):
two dentists in Nevada with an SEO score of at least 50%.

## Use it with

- **Clay, Make, Zapier, n8n:** start a run with Apify's own integrations
  and read the dataset, or call the Triremes API directly.
- **Your CRM:** export the dataset as CSV or JSON from Apify.

## Running it yourself

```sh
mkdir -p storage/key_value_stores/default
echo '{"apiKey":"your key","category":"Dentist","country":"US","limit":10}' > storage/key_value_stores/default/INPUT.json
go run .
ls storage/datasets/default/
```

It uses the Go standard library only. Tests: `go test ./...`.

The actor identifies itself to Triremes as `triremes-apify/1.0`.

MIT licence. Questions: [triremes.com/contact](https://triremes.com/contact).
