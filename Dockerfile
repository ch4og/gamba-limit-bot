FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o bot .

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /data
COPY --from=builder /app/bot /app/bot

VOLUME /data

ENV TELEGRAM_API_TOKEN=""
ENV ADMIN_USERNAME=""

CMD ["/app/bot"]
