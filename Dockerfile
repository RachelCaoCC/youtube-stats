FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vintage-social-counter .


FROM alpine:3.21

RUN addgroup -S app && adduser -S -G app app && apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/vintage-social-counter /app/vintage-social-counter
COPY --from=builder /src/index.html /app/index.html

RUN mkdir -p /app/data && chown -R app:app /app

USER app

ENV PORT=8080
ENV APP_ADDR=:8080
ENV SESSION_STORE_PATH=/app/data/sessions.json
ENV BOARD_STORE_PATH=/app/data/boards.json

EXPOSE 8080
VOLUME ["/app/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/app/vintage-social-counter"]
