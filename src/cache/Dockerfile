FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

WORKDIR /app

COPY . .

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 \
    GOFLAGS=-mod=vendor \
    GOPROXY=off \
    GOSUMDB=off \
    GOTOOLCHAIN=local

RUN go list -deps ./... >/dev/null
RUN GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" go build -trimpath -ldflags="-s -w" -o /cache .
RUN printf 'nonroot:x:65532:65532:nonroot:/nonexistent:/sbin/nologin\n' > /passwd \
    && printf 'nonroot:x:65532:\n' > /group

FROM scratch

WORKDIR /app

COPY --from=builder /passwd /etc/passwd
COPY --from=builder /group /etc/group
COPY --from=builder /cache /app/cache
COPY config.json /app/config.json

USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=15s CMD ["/app/cache", "healthcheck"]

CMD ["./cache"]
