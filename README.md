# cache ♻️

[![acceptance](https://github.com/the-hideout/cache/actions/workflows/acceptance.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/acceptance.yml) [![build](https://github.com/the-hideout/cache/actions/workflows/build.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/build.yml) [![lint](https://github.com/the-hideout/cache/actions/workflows/lint.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/lint.yml) [![test](https://github.com/the-hideout/cache/actions/workflows/test.yml/badge.svg)](https://github.com/the-hideout/cache/actions/workflows/test.yml)

A caching service using [crystal-lang](https://github.com/crystal-lang/crystal) + [Redis](https://redis.io/) with docker-compose

This service is used to cache all GraphQL responses from the main [Tarkov API](https://github.com/the-hideout/tarkov-api) in order to provide maximum performance ⚡

## About ⭐

This service exists to cache all response from the [Tarkov API](https://github.com/the-hideout/tarkov-api) for performance and to reduce load on our cloudflare workers. It is written in [crystal](https://github.com/crystal-lang/crystal) and is as simple as it needs to be.

This service caches requests only for a short period of time in order to keep data fresh and response times low

### How it Works 📚

This service works by doing the following:

- Recieving requests to save a graphql query in its in-memory cache (redis)
- Serving requests for cached graphql queries from its in-memory cache (redis)
- Expiring cached items at a fixed interval so they can be refreshed

Traffic flow:

1. Request hits the reverse proxy (caddy) - hosted on a VPS
2. The request is routed to the backend caching service (this caching service here!)
3. The request can either be a GET (retrieves from the cache) or a POST (saves to the cache)
4. GET requests fetch data from the redis cache and POST requests save data to the redis cache

## Usage 🔨

To use this repo do the following:

1. Clone the repo
2. Run the following command:

    ```bash
    docker-compose up --build
    ```

3. Create a request to the cache endpoint to set an item in the cache:

    ```bash
    curl --location --request POST 'http://localhost/api/cache' \
    --header 'Content-Type: application/json' \
    --data-raw '{
        "key": "mycoolquery",
        "value": "fake response"
    }'
    ```

4. Create a request to retrieve the item you just placed in the cache:

    ```bash
    curl --location --request GET 'http://localhost/api/cache?key=mycoolquery' \
    --header 'Content-Type: application/json' \
    --data-raw '{}'
    ```

5. As an added bonus, inspect your response headers to see how much longer the item will live in the cached before it expires and the request returns a 404 (`X-CACHE-TTL`)

That's it!

## Extra Info 📚

Here is some extra info about the setup!

### Volumes 🛢️

The docker-compose file creates one volume:

- `./data/redis:/data`

The config volume is used to mount Redis data so it can be persisted between container restarts in production.

## Contributing 🤝

To get started quickly with this project, you will need the following installed:

- [crystal](https://github.com/crystal-lang/crystal) ([crenv](https://github.com/crenv/crenv) is suggested)
- [docker compose](https://docs.docker.com/compose/)
- [bash](https://www.gnu.org/software/bash/)

To get your repo setup for development do the following:

1. Clone the repo
2. Ensure your version of crystal matches the version in [`.crystal-version`](.crystal-version)
3. Run the following command:

  ```bash
  script/bootstrap
  ```

1. Congrats you're ready to start developing!
2. Write some code
3. Run `script/accepance` to run acceptance test and ensure your changes will work
4. Open a pull request 🎉
