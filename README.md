# ShikiMAL Sync

ShikiMAL Sync is a small Go service that keeps your MyAnimeList list up to date with changes from Shikimori.

It works in one direction only:

`Shikimori -> MyAnimeList`

The idea is simple:
- the app reads your anime and manga list from Shikimori
- stores a local snapshot of the current state
- checks for changes on a timer
- sends only new, updated, or deleted entries to MyAnimeList

It does not try to do a full historical migration. On the first run it creates a baseline and starts syncing only the changes that happen after that.

## What it syncs

Anime:
- status
- score
- watched episodes
- deletion from the list

Manga:
- status
- score
- read chapters
- read volumes
- deletion from the list

Not synced in the current version:
- notes
- rewatch / reread flags
- dates

## Why it works this way

This project is focused on safe incremental sync.

That means it avoids mass overwrites on the first start. This is intentional: a blind full import can easily overwrite fields in MAL that are not present in Shikimori.

## Requirements

- Go 1.25 or newer
- a Shikimori OAuth app
- a MyAnimeList API app

## Config

The app reads settings from a JSON file `config.json`.

## OAuth setup

### Shikimori

Create an OAuth application and use:

- redirect URL: `http://127.0.0.1:18080/callback/shiki`
- scope: `user_rates`

### MyAnimeList

Create an API application and use:

- app type: `web`
- redirect URL: `http://127.0.0.1:18080/callback/mal`

## Build

```bash
go build -o shikimal-sync .
```

## Commands

Authorize Shikimori:

```bash
./shikimal-sync auth-shiki
```

Authorize MyAnimeList:

```bash
./shikimal-sync auth-mal
```

Create the initial local snapshot:

```bash
./shikimal-sync once
```

Run the sync loop:

```bash
./shikimal-sync run
```

## Local files

The app stores local data in the `data/` directory:

- `data/state.json` — current baseline snapshot
- `data/tokens/shikimori.json` — Shikimori OAuth token
- `data/tokens/mal.json` — MyAnimeList OAuth token

