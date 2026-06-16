FROM golang:1.22-alpine AS builder

RUN apk --no-cache add gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /daemon ./cmd/daemon

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata docker-cli
COPY --from=builder /daemon /usr/local/bin/quro-daemon

EXPOSE 8081
VOLUME ["/var/lib/quro", "/var/run/docker.sock"]

CMD ["quro-daemon"]
