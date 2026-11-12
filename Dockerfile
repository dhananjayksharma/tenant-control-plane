FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG CMD=api
RUN CGO_ENABLED=0 go build -trimpath -o /app ./cmd/${CMD}
FROM alpine:3.21
RUN adduser -D -u 10001 app
USER app
COPY --from=builder /app /app
ENTRYPOINT ["/app"]
