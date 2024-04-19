FROM crystallang/crystal:1.12.1-alpine as builder

WORKDIR /app

# add bash as alpine doesn't have it by default
RUN apk add bash

# copy core scripts
COPY script/preinstall script/preinstall
COPY script/bootstrap script/bootstrap
COPY script/postinstall script/postinstall

# copy all vendored dependencies
COPY lib/ lib/

# copy shard files
COPY shard.lock shard.lock
COPY shard.yml shard.yml

# bootstrap the project
RUN script/bootstrap

# copy all source files (ensure to use a .dockerignore file for efficient copying)
COPY . .

# build the project
RUN script/build

FROM crystallang/crystal:1.12.1-alpine

# add curl for healthchecks
RUN apk add curl

# create a non-root user for security
RUN adduser -D nonroot
USER nonroot

WORKDIR /app

######### CUSTOM SECTION PER PROJECT #########

# copy the binary from the builder stage
COPY --from=builder /app/bin/cache .

# copy the config file which the binary will use
COPY --chown=nonroot:nonroot config/config.json /app/config/config.json

# run the binary
CMD ["./cache"]
