FROM golang:1.24.3-alpine@sha256:b4f875e650466fa0fe62c6fd3f02517a392123eea85f1d7e69d85f780e4db1c1

WORKDIR /app

COPY . .

RUN go mod tidy
RUN go mod vendor
RUN go mod verify
RUN go build -mod=vendor -o cache

FROM golang:1.24.3-alpine@sha256:b4f875e650466fa0fe62c6fd3f02517a392123eea85f1d7e69d85f780e4db1c1

RUN apk add curl

RUN adduser -D nonroot
USER nonroot

WORKDIR /app

COPY --from=builder /app/cache .
COPY --chown=nonroot:nonroot config.json .

CMD ["./cache"]
