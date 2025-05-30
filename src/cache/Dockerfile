FROM golang:1.24.3-alpine as builder

WORKDIR /app

COPY . .

RUN go mod download
RUN go mod verify
RUN go build -o cache

FROM golang:1.24.3-alpine

RUN apk add curl

RUN adduser -D nonroot
USER nonroot

WORKDIR /app

COPY --from=builder /app/cache .
COPY --chown=nonroot:nonroot config.json .

CMD ["./cache"]
