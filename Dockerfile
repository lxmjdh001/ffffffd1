FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
COPY web/scripts/ scripts/
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/d1apk ./cmd/server

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 app && adduser -D -H -u 10001 -G app app && mkdir -p /app/data && chown app:app /app/data
WORKDIR /app
COPY --from=backend /out/d1apk /app/d1apk
COPY --from=web /src/web/dist /app/web/dist
USER 10001:10001
ENV HTTP_ADDR=0.0.0.0:8088 DATA_DIR=/app/data WEB_DIR=/app/web/dist
EXPOSE 8088
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/app/d1apk", "healthcheck"]
ENTRYPOINT ["/app/d1apk"]
