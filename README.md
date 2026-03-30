# cache ♻️

[![deploy](https://github.com/the-hideout/cache/actions/workflows/deploy.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/deploy.yml)
[![config validation](https://github.com/the-hideout/cache/actions/workflows/config-validation.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/config-validation.yml)
[![acceptance](https://github.com/the-hideout/cache/actions/workflows/acceptance.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/acceptance.yml)

A caching service using [Gin](https://github.com/gin-gonic/gin) + [Redis](https://redis.io/) with docker-compose

This service is used to cache all GraphQL responses from the main [Tarkov API](https://github.com/the-hideout/tarkov-api) in order to provide maximum performance ⚡

## About ⭐

This service exists to cache all response from the [Tarkov API](https://github.com/the-hideout/tarkov-api) for performance and to reduce load on our cloudflare workers. It is written in GoLang and is as simple as it needs to be.

This service caches requests only for a short period of time in order to keep data fresh and response times low.

### How it Works 📚

This service works by doing the following:

- Recieving requests to save a graphql query in its in-memory cache (redis)
- Serving requests for cached graphql queries from its in-memory cache (redis)
- Expiring cached items at a fixed interval so they can be refreshed

Traffic flow:

1. Request hits the standalone ingress proxy on the VPS
2. The request is routed to the backend cache service
3. The request can either be a GET (retrieves from the cache) or a POST (saves to the cache)

## Usage 🔨

To use this repo do the following:

1. Clone the repo
2. Run the following command:

    ```bash
    docker-compose up --build
    ```

3. Create a request to the cache endpoint to set an item in the cache:

    ```bash
    curl --location --request POST 'http://localhost:8080/api/cache' \
    --header 'Content-Type: application/json' \
    --data-raw '{
        "key": "mycoolquery",
        "value": "fake response"
    }'
    ```

4. Create a request to retrieve the item you just placed in the cache:

    ```bash
    curl --location --request GET 'http://localhost:8080/api/cache?key=mycoolquery' \
    --header 'Content-Type: application/json' \
    --data-raw '{}'
    ```

5. As an added bonus, inspect your response headers to see how much longer the item will live in the cached before it expires and the request returns a 404 (`X-CACHE-TTL`)

That's it!

## Production Routing

Production TLS and public routing for `cache.tarkov.dev` are handled by the standalone `the-hideout/ingress` repo on the shared Docker network named `ingress`.

This repo owns only the cache application and Redis stack.

## Extra Info 📚

Here is some extra info about the setup!

### Volumes 🛢️

The production docker-compose file persists Redis data at:

- `./data/redis:/data`

### Environment Variables 📝

Local development publishes the cache API on `localhost:8080` and Redis on `localhost:6379` through `docker-compose.override.yml`.

In production, the cache service joins the shared external `ingress` Docker network and does not publish host ports.
