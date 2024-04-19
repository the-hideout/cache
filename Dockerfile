FROM crystallang/crystal:1.12.1-alpine as builder

WORKDIR /app

RUN apk add bash

COPY script/bootstrap script/bootstrap
COPY lib/ lib/
COPY shard.lock shard.lock
COPY shard.yml shard.yml
RUN script/bootstrap

COPY . .

RUN script/build

FROM crystallang/crystal:1.12.1-alpine

RUN apk add curl

RUN adduser -D nonroot
USER nonroot

WORKDIR /app

COPY --from=builder /app/bin/cache .
COPY --chown=nonroot:nonroot config/config.json /app/config/config.json

CMD ["./cache"]
