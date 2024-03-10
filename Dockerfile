FROM golang:1.21.1-alpine as builder

WORKDIR /app

COPY . .

RUN go mod download
RUN go mod verify
RUN GOOS=linux GOARCH=amd64 go build -o main

FROM golang:1.21.1-alpine

RUN apk add curl

RUN adduser -D nonroot
USER nonroot

WORKDIR /app

COPY --from=builder /app/main .

CMD ["./main"]
